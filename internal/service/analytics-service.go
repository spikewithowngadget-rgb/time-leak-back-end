package service

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"time-leak/internal/domain"
	"time-leak/internal/repository"

	"go.uber.org/zap"
)

// AnalyticsService exposes read-only aggregate data for the admin dashboard.
// It never mutates state and never exposes raw phone numbers.
type AnalyticsService struct {
	repo *repository.Repository
	log  *zap.Logger
}

func NewAnalyticsService(repo *repository.Repository, log *zap.Logger) *AnalyticsService {
	if log == nil {
		log = zap.NewNop()
	}
	return &AnalyticsService{repo: repo, log: log}
}

// Overview returns aggregate counts for ads, devices and location events.
func (s *AnalyticsService) Overview(ctx context.Context) (domain.AnalyticsOverview, error) {
	ads, err := s.repo.AdsCounts(ctx)
	if err != nil {
		return domain.AnalyticsOverview{}, err
	}
	devices, err := s.repo.DeviceCounts(ctx)
	if err != nil {
		return domain.AnalyticsOverview{}, err
	}
	locations, err := s.repo.LocationCounts(ctx)
	if err != nil {
		return domain.AnalyticsOverview{}, err
	}
	return domain.AnalyticsOverview{Ads: ads, Devices: devices, Locations: locations}, nil
}

// Geo returns map points, approximate region grouping and headline summary.
func (s *AnalyticsService) Geo(ctx context.Context, filter domain.GeoFilter) (domain.GeoResult, error) {
	points, err := s.repo.ListGeoPoints(ctx, filter)
	if err != nil {
		return domain.GeoResult{}, err
	}
	for i := range points {
		points[i].PhoneMasked = maskPhoneNumber(points[i].Phone)
		points[i].Phone = ""
	}

	counts, err := s.repo.GeoCounts(ctx, filter)
	if err != nil {
		return domain.GeoResult{}, err
	}
	ios, android, err := s.repo.GeoPlatformDeviceCounts(ctx, filter)
	if err != nil {
		return domain.GeoResult{}, err
	}

	regions := groupRegions(points)
	topRegion := ""
	if len(regions) > 0 {
		topRegion = regions[0].Name
	}

	return domain.GeoResult{
		Summary: domain.GeoSummary{
			TotalEvents:    counts.Total,
			UniqueUsers:    counts.UniqueUsers,
			UniqueDevices:  counts.UniqueDevices,
			IOSDevices:     ios,
			AndroidDevices: android,
			TopRegion:      topRegion,
		},
		Regions: regions,
		Points:  points,
	}, nil
}

// ListDevices returns devices across all users with the real matching total.
func (s *AnalyticsService) ListDevices(ctx context.Context, filter domain.AdminDeviceListFilter) ([]domain.UserDevice, int, error) {
	return s.repo.ListDevices(ctx, filter)
}

// --- region grouping -------------------------------------------------------

// knownCity is an approximate anchor used only to label point clusters.
// These are NOT exact administrative boundaries; they are the nearest known
// city used as a human-friendly approximate label.
type knownCity struct {
	name string
	lat  float64
	lon  float64
}

// Major Kazakhstan cities used as approximate cluster labels.
var knownCities = []knownCity{
	{"Almaty", 43.2389, 76.8897},
	{"Astana", 51.1605, 71.4704},
	{"Shymkent", 42.3417, 69.5901},
	{"Karaganda", 49.8047, 73.1094},
	{"Aktobe", 50.2839, 57.1670},
	{"Taraz", 42.9000, 71.3667},
	{"Pavlodar", 52.2873, 76.9674},
	{"Oskemen", 49.9483, 82.6275},
	{"Semey", 50.4111, 80.2275},
	{"Atyrau", 47.0945, 51.9238},
	{"Kostanay", 53.2198, 63.6354},
	{"Kyzylorda", 44.8488, 65.4823},
	{"Oral", 51.2333, 51.3667},
	{"Petropavl", 54.8753, 69.1628},
	{"Aktau", 43.6410, 51.1980},
	{"Temirtau", 50.0547, 72.9644},
	{"Turkestan", 43.2973, 68.2517},
	{"Kokshetau", 53.2833, 69.3833},
	{"Taldykorgan", 45.0156, 78.3739},
	{"Ekibastuz", 51.7298, 75.3266},
}

// cityMatchRadiusKm is the generous radius for assigning a point to a known
// city label. Anything farther is grouped into an "Approximate area" bucket.
const cityMatchRadiusKm = 150.0

type regionAccumulator struct {
	name        string
	approximate bool
	count       int
	users       map[string]struct{}
	sumLat      float64
	sumLon      float64
	lastSeen    string // RFC3339; compared lexicographically (UTC, fixed format)
	region      domain.GeoRegion
}

func groupRegions(points []domain.GeoPoint) []domain.GeoRegion {
	if len(points) == 0 {
		return []domain.GeoRegion{}
	}

	buckets := make(map[string]*regionAccumulator)
	for _, p := range points {
		key, name, approximate := labelForPoint(p.Latitude, p.Longitude)
		acc := buckets[key]
		if acc == nil {
			acc = &regionAccumulator{name: name, approximate: approximate, users: make(map[string]struct{})}
			buckets[key] = acc
		}
		acc.count++
		acc.sumLat += p.Latitude
		acc.sumLon += p.Longitude
		if uid := strings.TrimSpace(p.UserID); uid != "" {
			acc.users[uid] = struct{}{}
		}
		if ts := p.CreatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"); ts > acc.lastSeen {
			acc.lastSeen = ts
			acc.region.LastSeenAt = p.CreatedAt
		}
	}

	total := float64(len(points))
	regions := make([]domain.GeoRegion, 0, len(buckets))
	for _, acc := range buckets {
		percentage := math.Round((float64(acc.count)/total)*1000) / 10
		regions = append(regions, domain.GeoRegion{
			Name:        acc.name,
			Approximate: acc.approximate,
			Count:       acc.count,
			UniqueUsers: len(acc.users),
			Percentage:  percentage,
			Center: domain.GeoCenter{
				Latitude:  acc.sumLat / float64(acc.count),
				Longitude: acc.sumLon / float64(acc.count),
			},
			LastSeenAt: acc.region.LastSeenAt,
		})
	}

	sort.Slice(regions, func(i, j int) bool {
		if regions[i].Count != regions[j].Count {
			return regions[i].Count > regions[j].Count
		}
		return regions[i].Name < regions[j].Name
	})
	return regions
}

// labelForPoint returns a grouping key, display name and whether the label is
// an approximate-area bucket (true) versus a nearest-known-city label (false).
func labelForPoint(lat, lon float64) (key string, name string, approximate bool) {
	nearest := -1
	nearestDist := math.MaxFloat64
	for i, city := range knownCities {
		d := haversineKm(lat, lon, city.lat, city.lon)
		if d < nearestDist {
			nearestDist = d
			nearest = i
		}
	}
	if nearest >= 0 && nearestDist <= cityMatchRadiusKm {
		city := knownCities[nearest]
		return "city:" + city.name, city.name, false
	}

	// Fall back to a coarse 1-degree grid bucket labeled as approximate.
	latBucket := math.Floor(lat)
	lonBucket := math.Floor(lon)
	key = fmt.Sprintf("approx:%.0f,%.0f", latBucket, lonBucket)
	name = fmt.Sprintf("Approximate area %.1f, %.1f", latBucket+0.5, lonBucket+0.5)
	return key, name, true
}

func haversineKm(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusKm = 6371.0
	dLat := degToRad(lat2 - lat1)
	dLon := degToRad(lon2 - lon1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(degToRad(lat1))*math.Cos(degToRad(lat2))*math.Sin(dLon/2)*math.Sin(dLon/2)
	return earthRadiusKm * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

func degToRad(deg float64) float64 {
	return deg * math.Pi / 180
}

// maskPhoneNumber hides the middle digits of a phone number.
func maskPhoneNumber(phone string) string {
	phone = strings.TrimSpace(phone)
	if len(phone) <= 4 {
		return phone
	}
	runes := []rune(phone)
	for i := 2; i < len(runes)-2; i++ {
		if runes[i] >= '0' && runes[i] <= '9' {
			runes[i] = '*'
		}
	}
	return string(runes)
}
