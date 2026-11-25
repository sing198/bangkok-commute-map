package http

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tk/bangkok-commute-map/backend/internal/cache"
	"github.com/tk/bangkok-commute-map/backend/internal/commute"
	"github.com/tk/bangkok-commute-map/backend/internal/db"
)

// OfficePreset represents a popular Bangkok office hub for one-click exploration.
type OfficePreset struct {
	ID          string  `json:"id"`
	NameTH      string  `json:"name_th"`
	NameEN      string  `json:"name_en"`
	SubtitleTH  string  `json:"subtitle_th"`
	Lat         float64 `json:"lat"`
	Lng         float64 `json:"lng"`
	NearestHint string  `json:"nearest_hint"`
}

var defaultPresets = []OfficePreset{
	{
		ID:          "asok",
		NameTH:      "อโศก",
		NameEN:      "Asok",
		SubtitleTH:  "แยกอโศก-สุขุมวิท (Exchange Tower / Interchange 21)",
		Lat:         13.7367,
		Lng:         100.5603,
		NearestHint: "BTS Asok / MRT Sukhumvit",
	},
	{
		ID:          "siam",
		NameTH:      "สยาม",
		NameEN:      "Siam",
		SubtitleTH:  "สยามพารากอน / สยามสแควร์",
		Lat:         13.7456,
		Lng:         100.5341,
		NearestHint: "BTS Siam",
	},
	{
		ID:          "silom",
		NameTH:      "สีลม",
		NameEN:      "Silom",
		SubtitleTH:  "สีลมคอมเพล็กซ์ / ย่านการเงิน",
		Lat:         13.7285,
		Lng:         100.5343,
		NearestHint: "BTS Sala Daeng / MRT Si Lom",
	},
	{
		ID:          "rama9",
		NameTH:      "พระราม 9",
		NameEN:      "Rama 9",
		SubtitleTH:  "G Tower / The 9th Towers / New CBD",
		Lat:         13.7579,
		Lng:         100.5653,
		NearestHint: "MRT Phra Ram 9",
	},
	{
		ID:          "ari",
		NameTH:      "อารีย์",
		NameEN:      "Ari",
		SubtitleTH:  "พหลโยธิน ซอย 7 / ย่านออฟฟิศและสตาร์ทอัพ",
		Lat:         13.7796,
		Lng:         100.5447,
		NearestHint: "BTS Ari",
	},
	{
		ID:          "one-bangkok",
		NameTH:      "วัน แบงค็อก / ลุมพินี",
		NameEN:      "One Bangkok / Lumphini",
		SubtitleTH:  "แยกวิทยุ-พระราม 4",
		Lat:         13.7263,
		Lng:         100.5469,
		NearestHint: "MRT Lumphini",
	},
}

// Server holds HTTP handler dependencies.
type Server struct {
	pool        *pgxpool.Pool
	cache       *cache.Client
	commuteSvc  *commute.Service
	allowOrigin string
}

// NewRouter constructs the Gin engine with middleware and MVP routes.
func NewRouter(pool *pgxpool.Pool, cacheClient *cache.Client, commuteSvc *commute.Service, allowOrigin string) *gin.Engine {
	s := &Server{
		pool:        pool,
		cache:       cacheClient,
		commuteSvc:  commuteSvc,
		allowOrigin: allowOrigin,
	}

	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	r.Use(s.corsMiddleware())
	r.Use(queryTimingMiddleware())

	api := r.Group("/api")
	{
		api.GET("/health", s.handleHealth)
		api.GET("/presets", s.handlePresets)
		api.GET("/commute/search", s.handleCommuteSearch)
		api.POST("/commute/compare", s.handleCommuteCompare)
		api.GET("/listings/:id/price-reports", s.handleListPriceReports)
		api.POST("/listings/:id/price-reports", s.handleSubmitPriceReport)
	}

	return r
}

func (s *Server) corsMiddleware() gin.HandlerFunc {
	allowed := strings.Split(s.allowOrigin, ",")
	for i := range allowed {
		allowed[i] = strings.TrimSpace(allowed[i])
	}

	return func(c *gin.Context) {
		reqOrigin := c.GetHeader("Origin")
		headerVal := "*"
		if len(allowed) > 0 && allowed[0] != "" && allowed[0] != "*" {
			headerVal = allowed[0]
			for _, a := range allowed {
				if a == reqOrigin && reqOrigin != "" {
					headerVal = reqOrigin
					break
				}
			}
		}

		c.Header("Access-Control-Allow-Origin", headerVal)
		c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
		c.Header("Access-Control-Expose-Headers", "X-Cache, X-Query-Time-Ms")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

type timingResponseWriter struct {
	gin.ResponseWriter
	start       time.Time
	headerAdded bool
}

func (w *timingResponseWriter) addTimingHeader() {
	if w.headerAdded {
		return
	}
	w.headerAdded = true
	elapsedMs := float64(time.Since(w.start).Microseconds()) / 1000.0
	w.Header().Set("X-Query-Time-Ms", strconv.FormatFloat(elapsedMs, 'f', 2, 64))
}

func (w *timingResponseWriter) WriteHeader(code int) {
	w.addTimingHeader()
	w.ResponseWriter.WriteHeader(code)
}

func (w *timingResponseWriter) Write(b []byte) (int, error) {
	w.addTimingHeader()
	return w.ResponseWriter.Write(b)
}

func (w *timingResponseWriter) WriteString(s string) (int, error) {
	w.addTimingHeader()
	return w.ResponseWriter.WriteString(s)
}

func queryTimingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		tw := &timingResponseWriter{
			ResponseWriter: c.Writer,
			start:          time.Now(),
		}
		c.Writer = tw
		c.Next()
	}
}

func (s *Server) handleHealth(c *gin.Context) {
	ctx := c.Request.Context()

	postgisStatus := "ok"
	postgisVersion := ""
	if s.pool == nil {
		postgisStatus = "down"
	} else {
		ver, err := db.CheckPostGIS(ctx, s.pool)
		if err != nil {
			postgisStatus = "down"
		} else {
			postgisVersion = ver
		}
	}

	redisStatus := "ok"
	if err := s.cache.Ping(ctx); err != nil {
		redisStatus = "degraded"
	}

	httpStatus := http.StatusOK
	overall := "ok"
	if postgisStatus != "ok" {
		httpStatus = http.StatusServiceUnavailable
		overall = "unhealthy"
	}

	c.JSON(httpStatus, gin.H{
		"status":          overall,
		"postgis":         postgisStatus,
		"postgis_version": postgisVersion,
		"redis":           redisStatus,
		"redis_mode":      s.cache.Mode(),
		"timestamp":       time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) handlePresets(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"presets": defaultPresets,
	})
}

func (s *Server) handleCommuteSearch(c *gin.Context) {
	if s.commuteSvc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "commute service not initialized"})
		return
	}

	lat, err := strconv.ParseFloat(c.Query("lat"), 64)
	if err != nil || lat < 13.45 || lat > 14.12 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "valid Greater Bangkok lat query parameter is required (13.45–14.12)"})
		return
	}
	lng, err := strconv.ParseFloat(c.Query("lng"), 64)
	if err != nil || lng < 100.25 || lng > 100.98 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "valid Greater Bangkok lng query parameter is required (100.25–100.98)"})
		return
	}

	maxMinutes, _ := strconv.Atoi(c.DefaultQuery("max_minutes", "45"))
	maxBudget, _ := strconv.Atoi(c.DefaultQuery("max_budget", "0"))
	workdays, _ := strconv.Atoi(c.DefaultQuery("workdays", "22"))
	walkMaxM, _ := strconv.Atoi(c.DefaultQuery("walk_max_m", "800"))
	sortBy := c.DefaultQuery("sort", "true_cost")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "450"))

	resp, cacheHit, err := s.commuteSvc.Search(c.Request.Context(), commute.SearchParams{
		Lat:        lat,
		Lng:        lng,
		MaxMinutes: maxMinutes,
		MaxBudget:  maxBudget,
		Workdays:   workdays,
		WalkMaxM:   walkMaxM,
		Sort:       sortBy,
		Limit:      limit,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if cacheHit {
		c.Header("X-Cache", "HIT")
	} else {
		c.Header("X-Cache", "MISS")
	}

	c.JSON(http.StatusOK, resp)
}

func (s *Server) handleCommuteCompare(c *gin.Context) {
	if s.commuteSvc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "commute service not initialized"})
		return
	}

	var req commute.CompareRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Office.Lat < 13.45 || req.Office.Lat > 14.12 || req.Office.Lng < 100.25 || req.Office.Lng > 100.98 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "office coordinates must be within Greater Bangkok"})
		return
	}

	resp, err := s.commuteSvc.Compare(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (s *Server) handleListPriceReports(c *gin.Context) {
	if s.commuteSvc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "commute service not initialized"})
		return
	}
	listingID := strings.TrimSpace(c.Param("id"))
	if listingID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "listing id is required"})
		return
	}
	resp, err := s.commuteSvc.ListPriceReports(c.Request.Context(), listingID)
	if err != nil {
		if errors.Is(err, commute.ErrListingNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "ไม่พบที่พักนี้ในระบบ"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (s *Server) handleSubmitPriceReport(c *gin.Context) {
	if s.commuteSvc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "commute service not initialized"})
		return
	}
	listingID := strings.TrimSpace(c.Param("id"))
	if listingID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "listing id is required"})
		return
	}
	var req commute.SubmitPriceReportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "รูปแบบข้อมูลไม่ถูกต้อง: " + err.Error()})
		return
	}

	clientIP := c.ClientIP()
	resp, err := s.commuteSvc.SubmitPriceReport(c.Request.Context(), listingID, req, clientIP)
	if err != nil {
		switch {
		case errors.Is(err, commute.ErrInvalidReport):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		case errors.Is(err, commute.ErrListingNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "ไม่พบที่พักนี้ในระบบ"})
		case errors.Is(err, commute.ErrRateLimited):
			c.JSON(http.StatusTooManyRequests, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	c.JSON(http.StatusCreated, resp)
}
