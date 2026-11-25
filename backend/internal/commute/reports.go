package commute

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

var (
	ErrListingNotFound = errors.New("listing not found")
	ErrRateLimited     = errors.New("rate limit exceeded")
	ErrInvalidReport   = errors.New("invalid price report")

	monthRe = regexp.MustCompile(`^[0-9]{4}-(0[1-9]|1[0-2])$`)
)

// SubmitPriceReportRequest is the payload for POST /api/listings/:id/price-reports.
type SubmitPriceReportRequest struct {
	MonthlyRentTHB int     `json:"monthly_rent_thb"`
	RoomSizeSqm    float64 `json:"room_size_sqm"`
	RoomType       string  `json:"room_type"`
	ReportedMonth  string  `json:"reported_month"`
	Note           string  `json:"note"`
}

// SubmitPriceReportResponse returns the created report plus refreshed aggregate stats.
type SubmitPriceReportResponse struct {
	Report      PriceReportRecord  `json:"report"`
	Stats       VerifiedPriceStats `json:"stats"`
	PriceStatus string             `json:"price_status"` // "verified" or "estimated"
	Message     string             `json:"message"`
}

// ListPriceReportsResponse returns anonymized reports and aggregate verified statistics.
type ListPriceReportsResponse struct {
	ListingID   string              `json:"listing_id"`
	PriceStatus string              `json:"price_status"`
	Stats       VerifiedPriceStats  `json:"stats"`
	Reports     []PriceReportRecord `json:"reports"`
}

// ValidatePriceReportInput checks field ranges and formats before touching the DB.
func ValidatePriceReportInput(req SubmitPriceReportRequest, now time.Time) error {
	if req.MonthlyRentTHB < 1500 || req.MonthlyRentTHB > 250000 {
		return fmt.Errorf("%w: ค่าเช่าต่อเดือนต้องอยู่ระหว่าง ฿1,500 – ฿250,000", ErrInvalidReport)
	}
	if req.RoomSizeSqm < 10 || req.RoomSizeSqm > 300 {
		return fmt.Errorf("%w: ขนาดห้องต้องอยู่ระหว่าง 10 – 300 ตร.ม.", ErrInvalidReport)
	}
	switch req.RoomType {
	case "studio", "1br", "2br", "dorm":
	default:
		return fmt.Errorf("%w: ประเภทห้องต้องเป็น studio, 1br, 2br หรือ dorm", ErrInvalidReport)
	}
	if !monthRe.MatchString(req.ReportedMonth) {
		return fmt.Errorf("%w: เดือนที่เช่าต้องอยู่ในรูปแบบ YYYY-MM (เช่น 2026-11)", ErrInvalidReport)
	}
	parsed, err := time.Parse("2006-01", req.ReportedMonth)
	if err != nil {
		return fmt.Errorf("%w: รูปแบบเดือนไม่ถูกต้อง", ErrInvalidReport)
	}
	currMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	oldestMonth := currMonth.AddDate(-2, 0, 0)
	if parsed.After(currMonth) {
		return fmt.Errorf("%w: เดือนที่เช่าต้องไม่เกินเดือนปัจจุบัน", ErrInvalidReport)
	}
	if parsed.Before(oldestMonth) {
		return fmt.Errorf("%w: กรุณารายงานราคาเช่าภายใน 24 เดือนล่าสุด", ErrInvalidReport)
	}
	if len([]rune(strings.TrimSpace(req.Note))) > 200 {
		return fmt.Errorf("%w: หมายเหตุต้องไม่เกิน 200 ตัวอักษร", ErrInvalidReport)
	}
	return nil
}

// IsRentOutlier flags extreme troll/typo submissions (< 0.35x or > 3.5x estimated baseline)
// so they are stored for audit without poisoning the public median rent.
func IsRentOutlier(reportedRentTHB int, estMin, estMax, estMid int) bool {
	base := estMid
	if base <= 0 && estMin > 0 && estMax > 0 {
		base = (estMin + estMax) / 2
	}
	if base <= 0 {
		return false
	}
	lowBound := int(float64(base) * 0.35)
	highBound := int(float64(base) * 3.50)
	if lowBound < 1500 {
		lowBound = 1500
	}
	return reportedRentTHB < lowBound || reportedRentTHB > highBound
}

// HashClientIP produces a privacy-preserving salted SHA-256 hash of a client IP.
func HashClientIP(rawIP string) string {
	salt := os.Getenv("PRICE_REPORT_SALT")
	if salt == "" {
		salt = "bangkok-commute-map-report-v1"
	}
	sum := sha256.Sum256([]byte(salt + ":" + strings.TrimSpace(rawIP)))
	return hex.EncodeToString(sum[:])
}

// SubmitPriceReport validates, rate-limits, flags outliers, stores a real tenant price report,
// and invalidates cached commute search responses when the verified median updates.
func (s *Service) SubmitPriceReport(
	ctx context.Context,
	listingID string,
	req SubmitPriceReportRequest,
	clientIP string,
) (SubmitPriceReportResponse, error) {
	req.RoomType = strings.ToLower(strings.TrimSpace(req.RoomType))
	req.ReportedMonth = strings.TrimSpace(req.ReportedMonth)
	req.Note = strings.TrimSpace(req.Note)

	if err := ValidatePriceReportInput(req, time.Now().UTC()); err != nil {
		return SubmitPriceReportResponse{}, err
	}

	ref, exists, err := s.repo.GetListingRentRef(ctx, listingID)
	if err != nil {
		return SubmitPriceReportResponse{}, err
	}
	if !exists {
		return SubmitPriceReportResponse{}, ErrListingNotFound
	}

	ipHash := HashClientIP(clientIP)

	// Per-IP hourly burst limit (max 10 reports/hour)
	recentCount, err := s.repo.CountRecentReportsByIP(ctx, ipHash)
	if err != nil {
		return SubmitPriceReportResponse{}, err
	}
	if recentCount >= 10 {
		return SubmitPriceReportResponse{}, fmt.Errorf("%w: ส่งรายงานครบโควตาต่อชั่วโมงแล้ว กรุณาลองใหม่ภายหลัง", ErrRateLimited)
	}

	// Per-(listing, IP) 30-day deduplication limit
	alreadyReported, err := s.repo.HasRecentListingReportByIP(ctx, listingID, ipHash)
	if err != nil {
		return SubmitPriceReportResponse{}, err
	}
	if alreadyReported {
		return SubmitPriceReportResponse{}, fmt.Errorf("%w: คุณได้รายงานราคาของที่พักนี้ไปแล้วภายใน 30 วันที่ผ่านมา", ErrRateLimited)
	}

	outlier := IsRentOutlier(req.MonthlyRentTHB, ref.EstimatedRentMinTHB, ref.EstimatedRentMaxTHB, ref.MonthlyRentTHB)
	rec, err := s.repo.InsertPriceReport(
		ctx, listingID, req.MonthlyRentTHB, req.RoomSizeSqm,
		req.RoomType, req.ReportedMonth, req.Note, ipHash, outlier,
	)
	if err != nil {
		return SubmitPriceReportResponse{}, err
	}

	stats, err := s.repo.GetVerifiedPriceStats(ctx, listingID)
	if err != nil {
		return SubmitPriceReportResponse{}, err
	}

	if !outlier && s.cache != nil {
		// Invalidate cached searches so the updated crowdsourced median appears right away.
		_, _ = s.cache.DeleteByPrefix(ctx, "commute:search:")
	}

	status := "estimated"
	if stats.ReportCount > 0 {
		status = "verified"
	}

	msg := "ขอบคุณที่แชร์ราคาเช่าจริง! ระบบอัปเดตค่ามัธยฐานสำหรับผู้ใช้งานคนอื่นเรียบร้อยแล้ว"
	if outlier {
		msg = "บันทึกข้อมูลแล้ว แต่ราคาต่างจากเกณฑ์ตลาดมาก ระบบจึงทำเครื่องหมายรอตรวจสอบและยังไม่นำมารวมในค่ามัธยฐาน"
	}

	return SubmitPriceReportResponse{
		Report:      rec,
		Stats:       stats,
		PriceStatus: status,
		Message:     msg,
	}, nil
}

// ListPriceReports fetches anonymized user price reports and aggregate stats for a listing.
func (s *Service) ListPriceReports(ctx context.Context, listingID string) (ListPriceReportsResponse, error) {
	_, exists, err := s.repo.GetListingRentRef(ctx, listingID)
	if err != nil {
		return ListPriceReportsResponse{}, err
	}
	if !exists {
		return ListPriceReportsResponse{}, ErrListingNotFound
	}

	stats, err := s.repo.GetVerifiedPriceStats(ctx, listingID)
	if err != nil {
		return ListPriceReportsResponse{}, err
	}
	reports, err := s.repo.ListPriceReports(ctx, listingID, 15)
	if err != nil {
		return ListPriceReportsResponse{}, err
	}
	if reports == nil {
		reports = []PriceReportRecord{}
	}

	status := "estimated"
	if stats.ReportCount > 0 {
		status = "verified"
	}

	return ListPriceReportsResponse{
		ListingID:   listingID,
		PriceStatus: status,
		Stats:       stats,
		Reports:     reports,
	}, nil
}
