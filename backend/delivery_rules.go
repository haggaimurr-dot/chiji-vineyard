package main

import (
	"errors"
	"time"
)

type ShippingRegion struct {
	Name                  string `json:"name"`
	Province              string `json:"province"`
	City                  string `json:"city"`
	District              string `json:"district"`
	Blocked               bool   `json:"blocked"`
	Freight               int    `json:"freight"`
	FreeShippingThreshold int    `json:"freeShippingThreshold"`
	BaseQuantity          int    `json:"baseQuantity"`
	ExtraFreight          int    `json:"extraFreight"`
	Surcharge             int    `json:"surcharge"`
}

func validateShippingDetails(r ShippingRule) error {
	if len(r.Regions) > 100 || len(r.BlockedDates) > 366 {
		return errors.New("配送规则数量超限")
	}
	seen := map[string]bool{}
	for _, x := range r.Regions {
		key := x.Province + "/" + x.City + "/" + x.District
		if !homeText(x.Name, 60) || !homeText(x.Province, 30) || (x.City != "" && !homeText(x.City, 30)) || (x.District != "" && (!homeText(x.District, 30) || x.City == "")) || seen[key] || x.BaseQuantity < 1 || x.BaseQuantity > 100000 || x.Freight < 0 || x.Freight > 10000000 || x.ExtraFreight < 0 || x.ExtraFreight > 10000000 || x.Surcharge < 0 || x.Surcharge > 10000000 || x.FreeShippingThreshold < 0 || x.FreeShippingThreshold > 10000000 {
			return errors.New("区域配送规则无效或地区重复")
		}
		seen[key] = true
	}
	seen = map[string]bool{}
	for _, date := range r.BlockedDates {
		t, e := time.Parse("2006-01-02", date)
		if e != nil || t.Format("2006-01-02") != date || seen[date] {
			return errors.New("不可配送日期无效或重复")
		}
		seen[date] = true
	}
	return nil
}

// District takes precedence over city, then province; province bans are absolute.
func calculateAddressQuote(items []OrderItem, rule ShippingRule, address Address, date string, now time.Time) (Quote, error) {
	q, err := calculateQuote(items, rule, address.Province)
	if err != nil {
		return q, err
	}
	quantity := 0
	for _, item := range items {
		quantity += item.Quantity
	}
	specificity := 0
	regionBlocked := false
	for _, x := range rule.Regions {
		if x.Province != address.Province || (x.City != "" && x.City != address.City) || (x.District != "" && x.District != address.District) {
			continue
		}
		score := 1
		if x.City != "" {
			score++
		}
		if x.District != "" {
			score++
		}
		if score <= specificity {
			continue
		}
		specificity = score
		q.ShippingRuleName = x.Name
		regionBlocked = x.Blocked
		q.FreeShippingThreshold = x.FreeShippingThreshold
		q.Freight = 0
		if q.Subtotal < x.FreeShippingThreshold {
			q.Freight = x.Freight + max(0, quantity-x.BaseQuantity)*x.ExtraFreight
		}
		q.Surcharge = x.Surcharge
		q.Freight += x.Surcharge
	}
	// Only the most specific regional ban applies, and never overrides a province ban.
	if regionBlocked {
		q.BlockedReason = "该地区暂不支持生鲜配送"
	}
	q.DeliveryMin = chinaDay(now.AddDate(0, 0, 2))
	q.DeliveryMax = chinaDay(now.AddDate(0, 0, 30))
	for _, item := range items {
		if item.SaleMode == "presale" && item.ExpectedShipDate > q.DeliveryMin {
			q.DeliveryMin = item.ExpectedShipDate
		}
	}
	if err = validateDelivery(date, items, now); err != nil {
		return q, err
	}
	for _, blocked := range rule.BlockedDates {
		if date != "" && date == blocked {
			q.BlockedReason = "所选日期暂停配送，请选择其他日期"
		}
	}
	q.Total = q.Subtotal + q.Freight
	return q, nil
}
