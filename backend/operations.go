package main

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
)

type Banner struct {
	Title    string `json:"title"`
	Copy     string `json:"copy"`
	Image    string `json:"image"`
	Button   string `json:"button"`
	Target   string `json:"target"`
	TargetID string `json:"targetId"`
}
type HomeConfig struct {
	Live             *OrchardLiveConfig `json:"live,omitempty"`
	CustomerService  CustomerService    `json:"customerService"`
	Activities       []HomeActivity     `json:"activities"`
	Announcements    []HomeAnnouncement `json:"announcements"`
	LaunchCalendar   []HomeLaunch       `json:"launchCalendar"`
	Banners          []Banner           `json:"banners"`
	ProductIDs       []string           `json:"productIds"`
	ArticleIDs       []string           `json:"articleIds"`
	StoryTitle       string             `json:"storyTitle"`
	StoryCopy        string             `json:"storyCopy"`
	CooperationTitle string             `json:"cooperationTitle"`
	CooperationCopy  string             `json:"cooperationCopy"`
}
type HomeActivity struct {
	Capacity           int    `json:"capacity,omitempty"`
	WaitlistEnabled    bool   `json:"waitlistEnabled,omitempty"`
	Cover              string `json:"cover,omitempty"`
	Location           string `json:"location,omitempty"`
	ID                 string `json:"id,omitempty"`
	ReservationEnabled bool   `json:"reservationEnabled,omitempty"`
	Title              string `json:"title"`
	Summary            string `json:"summary"`
	Content            string `json:"content"`
	StartDate          string `json:"startDate"`
	EndDate            string `json:"endDate"`
	ProductID          string `json:"productId"`
}
type HomeAnnouncement struct {
	Title       string `json:"title"`
	Content     string `json:"content"`
	PublishedAt string `json:"publishedAt"`
}
type HomeLaunch struct {
	Variety   string `json:"variety"`
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
	Note      string `json:"note"`
	ProductID string `json:"productId"`
}
type PrivacyPolicy struct {
	Title         string   `json:"title"`
	Version       string   `json:"version"`
	EffectiveDate string   `json:"effectiveDate"`
	Operator      string   `json:"operator"`
	Contact       string   `json:"contact"`
	Address       string   `json:"address"`
	Published     bool     `json:"published"`
	Content       []string `json:"content"`
}

//go:embed privacy.json
var privacyDefault []byte

func defaultHome() HomeConfig {
	return HomeConfig{Banners: []Banner{{Title: "把枝头的好味道，送到一家人的餐桌", Copy: "当季鲜果 · 按产期发货", Button: "选购当季葡萄", Target: "shop"}}, ProductIDs: []string{}, ArticleIDs: []string{}, StoryTitle: "一串葡萄，慢慢长成", StoryCopy: "了解葡萄的种植过程与成熟时间。", CooperationTitle: "团购、批发、经销合作", CooperationCopy: "按产期和渠道沟通采购需求"}
}
func (s *Store) loadConfig(ctx context.Context, key string, out any) error {
	var raw []byte
	err := s.db.QueryRowContext(ctx, "SELECT payload FROM configs WHERE config_key=?", key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}
func (s *Store) home(ctx context.Context) (HomeConfig, error) {
	if err := s.publishDueArticles(ctx, time.Now().UTC()); err != nil {
		return HomeConfig{}, err
	}
	h := defaultHome()
	err := s.loadConfig(ctx, "home", &h)
	return h, err
}
func (s *Store) privacy(ctx context.Context) (PrivacyPolicy, error) {
	var p PrivacyPolicy
	_ = json.Unmarshal(privacyDefault, &p)
	err := s.loadConfig(ctx, "privacy", &p)
	return p, err
}
func validateHome(h HomeConfig) error {
	if err := validateOrchardLive(h.Live); err != nil {
		return err
	}
	if err := validateCustomerService(h.CustomerService); err != nil {
		return err
	}
	if len(h.Activities) > 12 || len(h.Announcements) > 20 || len(h.LaunchCalendar) > 36 {
		return errors.New("活动最多 12 条、公告最多 20 条、上市安排最多 36 条")
	}
	activityIDs := map[string]bool{}
	for _, a := range h.Activities {
		if a.Capacity < 0 || a.Capacity > 100000 {
			return errors.New("活动容量须为0–100000的整数，0表示不限额")
		}
		if len(a.Cover) > 2000 || (a.Cover != "" && !strings.HasPrefix(a.Cover, "https://")) || len([]rune(a.Location)) > 200 {
			return errors.New("活动封面需为 HTTPS 地址，地点最多 200 字")
		}
		if (a.ReservationEnabled && a.ID == "") || (a.ID != "" && (!validActivityID(a.ID) || activityIDs[a.ID])) {
			return errors.New("预约活动需要唯一有效标识")
		}
		if a.ID != "" {
			activityIDs[a.ID] = true
		}
		if !homeText(a.Title, 80) || !homeText(a.Summary, 200) || !homeText(a.Content, 2000) || !homeDateRange(a.StartDate, a.EndDate) {
			return errors.New("活动标题、简介、正文或起止日期无效")
		}
	}
	for _, a := range h.Announcements {
		_, err := time.Parse("2006-01-02", a.PublishedAt)
		if !homeText(a.Title, 80) || !homeText(a.Content, 2000) || err != nil {
			return errors.New("公告标题、正文或发布日期无效")
		}
	}
	for _, a := range h.LaunchCalendar {
		if !homeText(a.Variety, 80) || len([]rune(a.Note)) > 200 || !homeDateRange(a.StartDate, a.EndDate) {
			return errors.New("上市品种、说明或起止日期无效")
		}
	}
	if len(h.Banners) < 1 || len(h.Banners) > 5 || len(h.ProductIDs) > 12 || len(h.ArticleIDs) > 6 {
		return errors.New("首页需要 1–5 个轮播位，最多 12 件商品和 6 篇文章")
	}
	for _, b := range h.Banners {
		if b.Image != "" && !strings.HasPrefix(b.Image, "https://") {
			return errors.New("轮播图片必须使用 HTTPS 地址")
		}
		if strings.TrimSpace(b.Title) == "" || len([]rune(b.Title)) > 80 || strings.TrimSpace(b.Button) == "" {
			return errors.New("运营位标题和按钮不能为空，标题最多 80 字")
		}
		switch b.Target {
		case "shop", "classroom", "cooperation":
		case "product", "article":
			if b.TargetID == "" {
				return errors.New("请选择运营位跳转内容")
			}
		default:
			return errors.New("运营位跳转类型无效")
		}
	}
	for _, ids := range [][]string{h.ProductIDs, h.ArticleIDs} {
		seen := map[string]bool{}
		for _, id := range ids {
			if id == "" || seen[id] {
				return errors.New("推荐内容不能重复或为空")
			}
			seen[id] = true
		}
	}
	return nil
}
func homeText(value string, max int) bool {
	return strings.TrimSpace(value) != "" && len([]rune(value)) <= max
}
func homeDateRange(start, end string) bool {
	first, err1 := time.Parse("2006-01-02", start)
	last, err2 := time.Parse("2006-01-02", end)
	return err1 == nil && err2 == nil && !last.Before(first) && last.Year()-first.Year() <= 5
}

func validatePrivacy(p PrivacyPolicy) error {
	if strings.TrimSpace(p.Title) == "" || strings.TrimSpace(p.Version) == "" || len(p.Content) < 10 {
		return errors.New("政策标题、版本及完整正文不能为空")
	}
	for _, section := range p.Content {
		if strings.TrimSpace(section) == "" {
			return errors.New("政策章节不能为空")
		}
	}
	if _, err := time.Parse("2006-01-02", p.EffectiveDate); err != nil {
		return errors.New("生效日期无效")
	}
	if p.Published && (strings.TrimSpace(p.Operator) == "" || strings.TrimSpace(p.Contact) == "" || strings.TrimSpace(p.Address) == "") {
		return errors.New("发布前请填写真实主体全称、隐私联系方式和地址")
	}
	return nil
}
func (s *Server) saveOperationalConfig(ctx context.Context, actor Claims, raw json.RawMessage, key string) (any, error) {
	if key != "home" && key != "privacy" {
		return nil, errors.New("配置类型无效")
	}
	var d map[string]json.RawMessage
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	result, err := s.saveVersionedConfig(ctx, actor, key, d[key])
	return result.Payload, err
}

type InventoryChange struct {
	SKUID         string `json:"skuId"`
	Stock         int    `json:"stock"`
	ExpectedStock *int   `json:"expectedStock"`
}

func validateInventory(items []InventoryChange) error {
	if len(items) == 0 || len(items) > 200 {
		return errors.New("一次提交 1–200 个规格")
	}
	seen := map[string]bool{}
	for _, i := range items {
		if i.SKUID == "" || seen[i.SKUID] || i.Stock < 0 || i.Stock > 2147483647 || i.ExpectedStock == nil || *i.ExpectedStock < 0 {
			return errors.New("库存参数无效或规格重复")
		}
		seen[i.SKUID] = true
	}
	return nil
}
func (s *Server) batchInventory(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	var d struct {
		Items  []InventoryChange `json:"items"`
		Reason string            `json:"reason"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if err := validateInventory(d.Items); err != nil {
		return nil, err
	}
	d.Reason = clean(d.Reason, 200)
	if d.Reason == "" {
		return nil, errors.New("请填写调整原因")
	}
	sort.Slice(d.Items, func(i, j int) bool { return d.Items[i].SKUID < d.Items[j].SKUID })
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for _, i := range d.Items {
		var stock int
		if err = tx.QueryRowContext(ctx, "SELECT stock FROM skus WHERE id=? AND active=1 FOR UPDATE", i.SKUID).Scan(&stock); err != nil {
			return nil, errors.New("规格不存在或已停用")
		}
		if stock != *i.ExpectedStock {
			return nil, errors.New("库存已变化，请刷新后重新调整；本批次未保存")
		}
		if _, err = applyInventory(ctx, tx, i.SKUID, "ADJUST", "", actor.Username, d.Reason, "UPDATE skus SET stock=? WHERE id=?", i.Stock, i.SKUID); err != nil {
			return nil, err
		}
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO audit_logs(admin_id,username,action,target_id,detail) VALUES(?,?,?,?,?)", actor.Subject, actor.Username, "INVENTORY_BATCH", "batch", jsonValue(d)); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return M{"updated": len(d.Items)}, nil
}
