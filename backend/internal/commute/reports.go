package commute

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
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
	SourceURL      string  `json:"source_url"`
}

// SubmitPriceReportResponse returns the created report plus refreshed aggregate stats.
type SubmitPriceReportResponse struct {
	Report      PriceReportRecord  `json:"report"`
	Stats       VerifiedPriceStats `json:"stats"`
	PriceStatus string             `json:"price_status"` // "verified" or "unverified"
	Message     string             `json:"message"`
}

// ListPriceReportsResponse returns anonymized reports and aggregate verified statistics.
type ListPriceReportsResponse struct {
	ListingID   string              `json:"listing_id"`
	PriceStatus string              `json:"price_status"`
	Stats       VerifiedPriceStats  `json:"stats"`
	Reports     []PriceReportRecord `json:"reports"`
}

// ValidatePriceReportInput checks field ranges, formats, and safe HTTP(S) URLs before touching the DB.
func ValidatePriceReportInput(req SubmitPriceReportRequest, now time.Time) error {
	if req.MonthlyRentTHB < 1500 || req.MonthlyRentTHB > 500000 {
		return fmt.Errorf("%w: ค่าเช่าต่อเดือนต้องอยู่ระหว่าง ฿1,500 – ฿500,000", ErrInvalidReport)
	}
	if req.RoomSizeSqm < 10 || req.RoomSizeSqm > 500 {
		return fmt.Errorf("%w: ขนาดห้องต้องอยู่ระหว่าง 10 – 500 ตร.ม.", ErrInvalidReport)
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
	if src := strings.TrimSpace(req.SourceURL); src != "" {
		if len(src) > 500 {
			return fmt.Errorf("%w: ลิงก์อ้างอิงต้องไม่เกิน 500 ตัวอักษร", ErrInvalidReport)
		}
		u, err := url.ParseRequestURI(src)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("%w: ลิงก์อ้างอิงต้องขึ้นต้นด้วย https:// หรือ http://", ErrInvalidReport)
		}
	}
	return nil
}

// IsRentOutlierAgainstMedian flags extreme typo/troll submissions ONLY when at least 3 verified
// user reports already exist and the new rent is < 0.25x or > 4.0x the crowdsourced median.
// It never rejects genuine luxury serviced residences or budget dorms based on guesses.
func IsRentOutlierAgainstMedian(reportedRentTHB int, existing VerifiedPriceStats) bool {
	if existing.ReportCount < 3 || existing.MedianRentTHB <= 0 {
		return false
	}
	lowBound := int(float64(existing.MedianRentTHB) * 0.25)
	highBound := int(float64(existing.MedianRentTHB) * 4.0)
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

// SubmitPriceReport validates, rate-limits, checks crowdsourced outliers, stores a real tenant price report
// (with optional building/listing URL), and invalidates cached commute search responses.
func (s *Service) SubmitPriceReport(
	ctx context.Context,
	listingID string,
	req SubmitPriceReportRequest,
	clientIP string,
) (SubmitPriceReportResponse, error) {
	req.RoomType = strings.ToLower(strings.TrimSpace(req.RoomType))
	req.ReportedMonth = strings.TrimSpace(req.ReportedMonth)
	req.Note = strings.TrimSpace(req.Note)
	req.SourceURL = strings.TrimSpace(req.SourceURL)

	if err := ValidatePriceReportInput(req, time.Now().UTC()); err != nil {
		return SubmitPriceReportResponse{}, err
	}

	_, exists, err := s.repo.GetListingRentRef(ctx, listingID)
	if err != nil {
		return SubmitPriceReportResponse{}, err
	}
	if !exists {
		return SubmitPriceReportResponse{}, ErrListingNotFound
	}

	ipHash := HashClientIP(clientIP)

	recentCount, err := s.repo.CountRecentReportsByIP(ctx, ipHash)
	if err != nil {
		return SubmitPriceReportResponse{}, err
	}
	if recentCount >= 10 {
		return SubmitPriceReportResponse{}, fmt.Errorf("%w: ส่งรายงานครบโควตาต่อชั่วโมงแล้ว กรุณาลองใหม่ภายหลัง", ErrRateLimited)
	}

	alreadyReported, err := s.repo.HasRecentListingReportByIP(ctx, listingID, ipHash)
	if err != nil {
		return SubmitPriceReportResponse{}, err
	}
	if alreadyReported {
		return SubmitPriceReportResponse{}, fmt.Errorf("%w: คุณได้รายงานราคาของที่พักนี้ไปแล้วภายใน 30 วันที่ผ่านมา", ErrRateLimited)
	}

	existingStats, err := s.repo.GetVerifiedPriceStats(ctx, listingID)
	if err != nil {
		return SubmitPriceReportResponse{}, err
	}
	outlier := IsRentOutlierAgainstMedian(req.MonthlyRentTHB, existingStats)

	rec, err := s.repo.InsertPriceReport(
		ctx, listingID, req.MonthlyRentTHB, req.RoomSizeSqm,
		req.RoomType, req.ReportedMonth, req.Note, req.SourceURL, ipHash, outlier,
	)
	if err != nil {
		return SubmitPriceReportResponse{}, err
	}

	stats, err := s.repo.GetVerifiedPriceStats(ctx, listingID)
	if err != nil {
		return SubmitPriceReportResponse{}, err
	}

	if !outlier && s.cache != nil {
		_, _ = s.cache.DeleteByPrefix(ctx, "commute:search:")
	}

	status := "unverified"
	if stats.ReportCount > 0 {
		status = "verified"
	}

	msg := "ขอบคุณที่แชร์ราคาเช่าจริง! ระบบอัปเดตข้อมูลให้ผู้ใช้งานคนอื่นเรียบร้อยแล้ว"
	if outlier {
		msg = "บันทึกข้อมูลแล้ว แต่ราคาต่างจากรายงานผู้เช่าเดิมมาก ระบบจึงแยกไว้ตรวจสอบก่อนนำมารวมในค่ามัธยฐาน"
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

	status := "unverified"
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
