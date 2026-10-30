package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

type M = map[string]any

type SKU struct {
	ExpectedStock  *int   `json:"expectedStock,omitempty"`
	ID             string `json:"id"`
	ProductID      string `json:"productId,omitempty"`
	Name           string `json:"name"`
	Price          int    `json:"price"`
	CompareAtPrice int    `json:"compareAtPrice,omitempty"`
	Stock          int    `json:"stock"`
	Reserved       int    `json:"reserved,omitempty"`
	Sold           int    `json:"sold,omitempty"`
	LimitPerOrder  int    `json:"limitPerOrder"`
	WeightLabel    string `json:"weightLabel"`
	Active         bool   `json:"active"`
	SortOrder      int    `json:"sortOrder,omitempty"`
}
type Product struct {
	PriceDropReminder bool               `json:"priceDropReminder"`
	PriceDrop         *PriceDrop         `json:"priceDrop,omitempty"`
	LinkedArticles    []linkedArticle     `json:"linkedArticles,omitempty"`
	Favorite          bool               `json:"favorite"`
	RestockReminder   bool               `json:"restockReminder"`
	RestockPushStatus string             `json:"restockPushStatus,omitempty"`
	ID                string             `json:"id"`
	Name              string             `json:"name"`
	Subtitle          string             `json:"subtitle"`
	Category          string             `json:"category"`
	SaleMode          string             `json:"saleMode"`
	ExpectedShipDate  string             `json:"expectedShipDate,omitempty"`
	Cover             string             `json:"cover"`
	Gallery           []string           `json:"gallery"`
	Badges            []string           `json:"badges"`
	Origin            string             `json:"origin"`
	Variety           string             `json:"variety,omitempty"`
	Storage           string             `json:"storage,omitempty"`
	NetWeightNote     string             `json:"netWeightNote,omitempty"`
	ImageNote         string             `json:"imageNote,omitempty"`
	Description       string             `json:"description"`
	Highlights        []string           `json:"highlights"`
	Traceability      *Traceability      `json:"traceability,omitempty"`
	GiftCustomization *GiftCustomization `json:"giftCustomization,omitempty"`
	Pairings          []Pairing          `json:"pairings,omitempty"`
	Status            string             `json:"status"`
	SortOrder         int                `json:"sortOrder"`
	SKUs              []SKU              `json:"skus"`
}
type Address struct {
	ID        string `json:"id,omitempty"`
	IsDefault bool   `json:"isDefault"`
	Name      string `json:"name"`
	Phone     string `json:"phone"`
	Province  string `json:"province"`
	City      string `json:"city"`
	District  string `json:"district"`
	Detail    string `json:"detail"`
}
type CartItem struct {
	ProductID string   `json:"productId"`
	SKUID     string   `json:"skuId"`
	Quantity  int      `json:"quantity"`
	Selected  bool     `json:"selected"`
	Product   *Product `json:"product,omitempty"`
	SKU       *SKU     `json:"sku,omitempty"`
}
type OrderItem struct {
	CartItem
	Name             string `json:"name"`
	Cover            string `json:"cover"`
	SaleMode         string `json:"saleMode"`
	ExpectedShipDate string `json:"expectedShipDate"`
	SKUName          string `json:"skuName"`
	WeightLabel      string `json:"weightLabel"`
	UnitPrice        int    `json:"unitPrice"`
}
type Quote struct {
	ShippingRuleName      string `json:"shippingRuleName,omitempty"`
	Surcharge             int    `json:"surcharge"`
	DeliveryMin           string `json:"deliveryMin,omitempty"`
	DeliveryMax           string `json:"deliveryMax,omitempty"`
	Discount              int    `json:"discount"`
	CouponCode            string `json:"couponCode,omitempty"`
	Subtotal              int    `json:"subtotal"`
	Freight               int    `json:"freight"`
	Total                 int    `json:"total"`
	FreeShippingThreshold int    `json:"freeShippingThreshold"`
	BlockedReason         string `json:"blockedReason,omitempty"`
}
type ShippingRule struct {
	Regions               []ShippingRegion `json:"regions,omitempty"`
	BlockedDates          []string         `json:"blockedDates,omitempty"`
	Freight               int              `json:"freight"`
	FreeShippingThreshold int              `json:"freeShippingThreshold"`
	BlockedProvinces      []string         `json:"blockedProvinces"`
}
type Shipment struct {
	Parcels       []Shipment `json:"parcels,omitempty"`
	Carrier       string     `json:"carrier"`
	CarrierCode   string     `json:"carrierCode"`
	TrackingNo    string     `json:"trackingNo"`
	ShippedAt     string     `json:"shippedAt"`
	SyncStatus    string     `json:"syncStatus"`
	SyncError     string     `json:"syncError,omitempty"`
	SyncAttempt   string     `json:"syncAttempt,omitempty"`
	SyncStartedAt string     `json:"syncStartedAt,omitempty"`
}
type Order struct {
	Packages             []OrderPackage `json:"packages,omitempty"`
	PackageVersion       int            `json:"packageVersion,omitempty"`
	Events               []OrderEvent   `json:"events,omitempty"`
	Batches              []OrderBatch   `json:"batches,omitempty"`
	BatchVersion         int            `json:"batchVersion,omitempty"`
	ShippingNoticeStatus string         `json:"shippingNoticeStatus,omitempty"`
	ShippingNoticeError  string         `json:"shippingNoticeError,omitempty"`
	RefundedAmount       int            `json:"refundedAmount"`
	DeliveryDate         string         `json:"deliveryDate,omitempty"`
	Invoice              *Invoice       `json:"invoice,omitempty"`
	ID                   string         `json:"id"`
	OrderNo              string         `json:"orderNo"`
	OpenID               string         `json:"-"`
	CreatedAt            string         `json:"createdAt"`
	OrderStatus          string         `json:"orderStatus"`
	PaymentStatus        string         `json:"paymentStatus"`
	FulfillmentStatus    string         `json:"fulfillmentStatus"`
	RefundStatus         string         `json:"refundStatus"`
	Items                []OrderItem    `json:"items"`
	Address              Address        `json:"address"`
	Quote                Quote          `json:"quote"`
	Note                 string         `json:"note,omitempty"`
	Shipment             *Shipment      `json:"shipment,omitempty"`
	PaymentAttempted     bool           `json:"-"`
	PaidAt               string         `json:"paidAt,omitempty"`
	TransactionID        string         `json:"-"`
	ExpiresAt            time.Time      `json:"-"`
}
type Article struct {
	Completed    bool     `json:"completed"`
	Tags         []string `json:"tags,omitempty"`
	ProductIDs   []string `json:"productIds,omitempty"`
	Products     []linkedProduct `json:"products,omitempty"`
	ReviewStatus string   `json:"reviewStatus,omitempty"`
	ReviewNote   string   `json:"reviewNote,omitempty"`
	ReviewedBy   string   `json:"reviewedBy,omitempty"`
	ReviewedAt   string   `json:"reviewedAt,omitempty"`
	Favorite     bool     `json:"favorite,omitempty"`
	LastReadAt   string   `json:"lastReadAt,omitempty"`
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Summary      string   `json:"summary"`
	Cover        string   `json:"cover"`
	Variety      string   `json:"variety"`
	Stage        string   `json:"stage"`
	Type         string   `json:"type"`
	Duration     string   `json:"duration,omitempty"`
	PublishedAt  string   `json:"publishedAt"`
	Content      []string `json:"content"`
	ContentHTML  string   `json:"contentHtml,omitempty"`
	VideoURL     string   `json:"videoUrl,omitempty"`
	Status       string   `json:"status"`
	Version      int      `json:"version"`
	ScheduledAt  string   `json:"scheduledAt,omitempty"`
	UpdatedAt    string   `json:"updatedAt,omitempty"`
	ReadCount    int      `json:"readCount,omitempty"`
	ReaderCount  int      `json:"readerCount,omitempty"`
}
type CustomerProfile struct {
	Email    string `json:"email"`
	Address  string `json:"address"`
	Industry string `json:"industry"`
	Notes    string `json:"notes"`
}
type Lead struct {
	Contacts          []LeadContact   `json:"contacts"`
	Contracts         []LeadContract  `json:"contracts"`
	Receipts          []LeadReceipt   `json:"receipts"`
	ExternalReminder  bool            `json:"externalReminder"`
	CustomerProfile   CustomerProfile `json:"customerProfile"`
	OpportunityAmount int64           `json:"opportunityAmount"`
	ID                string          `json:"id"`
	Organization      string          `json:"organization"`
	Contact           string          `json:"contact"`
	Phone             string          `json:"phone"`
	Region            string          `json:"region"`
	Channel           string          `json:"channel"`
	Volume            string          `json:"volume"`
	Demand            string          `json:"demand"`
	Status            string          `json:"status"`
	FollowUpNote      string          `json:"followUpNote"`
	FollowUps         []LeadFollowUp  `json:"followUps"`
	OwnerID           string          `json:"ownerId"`
	OwnerName         string          `json:"ownerName"`
	ReminderAt        string          `json:"reminderAt"`
	ReminderDone      bool            `json:"reminderDone"`
	Version           int             `json:"version"`
	CreatedAt         string          `json:"createdAt"`
}
type LeadFollowUp struct {
	ID             string `json:"id"`
	Note           string `json:"note"`
	Status         string `json:"status"`
	PreviousStatus string `json:"previousStatus"`
	CreatedAt      string `json:"createdAt"`
	Username       string `json:"username"`
	Legacy         bool   `json:"legacy,omitempty"`
}
type Refund struct {
	OrderTotal        int      `json:"orderTotal"`
	FulfillmentStatus string   `json:"fulfillmentStatus"`
	Images            []string `json:"images"`
	ID                string   `json:"id"`
	OrderID           string   `json:"orderId"`
	OrderNo           string   `json:"orderNo"`
	OpenID            string   `json:"-"`
	Amount            int      `json:"amount"`
	Reason            string   `json:"reason"`
	Note              string   `json:"note"`
	Status            string   `json:"status"`
	StockRestored     bool     `json:"-"`
	ProviderRefundID  string   `json:"providerRefundId,omitempty"`
	RejectReason      string   `json:"rejectReason,omitempty"`
	ReviewedBy        string   `json:"-"`
	CreatedAt         string   `json:"createdAt"`
	UpdatedAt         string   `json:"updatedAt"`
}
type Admin struct {
	Permissions  []string `json:"permissions"`
	ID           string   `json:"id"`
	Username     string   `json:"username"`
	PasswordHash string   `json:"-"`
	Role         string   `json:"role"`
	Active       bool     `json:"active"`
	CreatedAt    string   `json:"createdAt"`
	LastLoginAt  *string  `json:"lastLoginAt,omitempty"`
}
type Claims struct {
	SessionID  string `json:"sid,omitempty"`
	Subject    string `json:"sub"`
	Kind       string `json:"kind"`
	Username   string `json:"username,omitempty"`
	Role       string `json:"role,omitempty"`
	Credential string `json:"credential,omitempty"`
	Exp        int64  `json:"exp"`
}
type Request struct {
	Action string          `json:"action"`
	Data   json.RawMessage `json:"data"`
}
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

var phoneRE = regexp.MustCompile(`^1\d{10}$`)

func decode(raw json.RawMessage, out any) error {
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	return json.Unmarshal(raw, out)
}
func clean(value string, max int) string {
	value = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(value, "<", ""), ">", ""))
	r := []rune(value)
	if len(r) > max {
		return string(r[:max])
	}
	return value
}
func validateAddress(a Address) (Address, error) {
	a.Name = clean(a.Name, 30)
	a.Phone = clean(a.Phone, 20)
	a.Province = clean(a.Province, 30)
	a.City = clean(a.City, 30)
	a.District = clean(a.District, 30)
	a.Detail = clean(a.Detail, 120)
	if a.Name == "" || !phoneRE.MatchString(a.Phone) || a.Province == "" || a.City == "" || a.Detail == "" {
		return a, errors.New("收货地址不完整")
	}
	return a, nil
}
func calculateQuote(items []OrderItem, rule ShippingRule, province string) (Quote, error) {
	if len(items) == 0 {
		return Quote{}, errors.New("订单至少需要一件商品")
	}
	q := Quote{FreeShippingThreshold: rule.FreeShippingThreshold}
	for _, i := range items {
		if i.Quantity < 1 || i.Quantity > i.SKU.LimitPerOrder {
			return q, fmt.Errorf("%s超过限购数量", i.Name)
		}
		if i.Quantity > i.SKU.Stock {
			return q, fmt.Errorf("%s库存不足", i.Name)
		}
		q.Subtotal += i.UnitPrice * i.Quantity
	}
	for _, p := range rule.BlockedProvinces {
		if p == province {
			q.BlockedReason = "该地区暂不支持生鲜配送"
		}
	}
	if q.Subtotal < rule.FreeShippingThreshold {
		q.Freight = rule.Freight
	}
	q.Total = q.Subtotal + q.Freight
	return q, nil
}
func canRefund(o Order) bool {
	return o.PaymentStatus == "PAID" && o.RefundedAmount < o.Quote.Total && (o.FulfillmentStatus == "UNFULFILLED" || o.FulfillmentStatus == "PREPARING" || o.FulfillmentStatus == "SHIPPED" || o.FulfillmentStatus == "RECEIVED") && (o.RefundStatus == "NONE" || o.RefundStatus == "REJECTED" || o.RefundStatus == "PARTIAL")
}
func canShip(o Order) bool {
	return o.PaymentStatus == "PAID" && (o.FulfillmentStatus == "UNFULFILLED" || o.FulfillmentStatus == "PREPARING") && (o.RefundStatus == "NONE" || o.RefundStatus == "REJECTED" || o.RefundStatus == "PARTIAL")
}
func nowString() string { return time.Now().UTC().Format(time.RFC3339Nano) }
