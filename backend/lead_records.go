package main

import (
	"errors"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"
)

type LeadContact struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Phone   string `json:"phone"`
	Email   string `json:"email"`
	Title   string `json:"title"`
	Primary bool   `json:"primary"`
}
type LeadContract struct {
	ID       string `json:"id"`
	Number   string `json:"number"`
	Amount   int64  `json:"amount"`
	SignedAt string `json:"signedAt"`
	DueAt    string `json:"dueAt"`
	Status   string `json:"status"`
}

// Receipts are append-only accounting records. Corrections require a new business process.
type LeadReceipt struct {
	ID         string `json:"id"`
	ContractID string `json:"contractId"`
	Amount     int64  `json:"amount"`
	ReceivedAt string `json:"receivedAt"`
	Reference  string `json:"reference"`
}

func crmText(v *string, limit int, required bool) error {
	*v = strings.TrimSpace(*v)
	if utf8.RuneCountInString(*v) > limit || (required && *v == "") {
		return errors.New("CRM 内容为空或过长")
	}
	return nil
}
func crmDate(v string, required bool) bool {
	if v == "" {
		return !required
	}
	_, e := time.Parse("2006-01-02", v)
	return e == nil
}
func applyLeadCRM(next *Lead, current Lead, d leadUpdate) ([]string, error) {
	changes := []string{}
	if d.Contacts != nil {
		next.Contacts = append([]LeadContact{}, (*d.Contacts)...)
	}
	if d.Contracts != nil {
		next.Contracts = append([]LeadContract{}, (*d.Contracts)...)
	}
	if d.Receipts != nil {
		next.Receipts = append([]LeadReceipt{}, (*d.Receipts)...)
	}
	if d.ExternalReminder != nil {
		next.ExternalReminder = *d.ExternalReminder
	}
	if len(next.Contacts) > 50 || len(next.Contracts) > 100 || len(next.Receipts) > 500 {
		return nil, errors.New("联系人/合同/回款记录数量超限")
	}
	ids := map[string]bool{}
	primary := 0
	for i := range next.Contacts {
		c := &next.Contacts[i]
		for _, f := range []struct {
			v        *string
			n        int
			required bool
		}{{&c.ID, 80, true}, {&c.Name, 100, true}, {&c.Phone, 50, false}, {&c.Email, 200, false}, {&c.Title, 100, false}} {
			if e := crmText(f.v, f.n, f.required); e != nil {
				return nil, e
			}
		}
		if ids[c.ID] {
			return nil, errors.New("联系人编号重复")
		}
		ids[c.ID] = true
		p := CustomerProfile{Email: c.Email}
		if e := validateCustomerProfile(&p); e != nil {
			return nil, e
		}
		if c.Primary {
			primary++
		}
	}
	if primary > 1 {
		return nil, errors.New("只能设置一个主要联系人")
	}
	contracts := map[string]LeadContract{}
	numbers := map[string]bool{}
	for i := range next.Contracts {
		c := &next.Contracts[i]
		if e := crmText(&c.ID, 80, true); e != nil {
			return nil, e
		}
		if e := crmText(&c.Number, 100, true); e != nil {
			return nil, e
		}
		if _, ok := contracts[c.ID]; ok || numbers[c.Number] {
			return nil, errors.New("合同编号重复")
		}
		if c.Amount <= 0 || c.Amount > 100000000000 {
			return nil, errors.New("合同金额必须大于零且不超过10亿元")
		}
		if c.Status != "DRAFT" && c.Status != "SIGNED" && c.Status != "CANCELLED" {
			return nil, errors.New("合同状态无效")
		}
		if !crmDate(c.SignedAt, c.Status == "SIGNED") || !crmDate(c.DueAt, false) || (c.DueAt != "" && c.SignedAt != "" && c.DueAt < c.SignedAt) {
			return nil, errors.New("合同日期无效")
		}
		contracts[c.ID] = *c
		numbers[c.Number] = true
	}
	receipts := map[string]LeadReceipt{}
	totals := map[string]int64{}
	refs := map[string]bool{}
	for i := range next.Receipts {
		r := &next.Receipts[i]
		if e := crmText(&r.ID, 80, true); e != nil {
			return nil, e
		}
		if e := crmText(&r.Reference, 200, true); e != nil {
			return nil, e
		}
		if _, ok := receipts[r.ID]; ok || refs[r.Reference] {
			return nil, errors.New("回款编号或流水号重复")
		}
		c, ok := contracts[r.ContractID]
		if !ok || c.Status != "SIGNED" {
			return nil, errors.New("回款必须关联已签订合同")
		}
		if r.Amount <= 0 || r.Amount > 100000000000 || !crmDate(r.ReceivedAt, true) {
			return nil, errors.New("回款金额或日期无效")
		}
		totals[c.ID] += r.Amount
		if totals[c.ID] > c.Amount {
			return nil, errors.New("累计回款不能超过合同金额")
		}
		receipts[r.ID] = *r
		refs[r.Reference] = true
	}
	for _, r := range current.Receipts {
		if v, ok := receipts[r.ID]; !ok || v != r {
			return nil, errors.New("已登记回款不可修改或删除")
		}
	}
	for _, field := range []struct {
		name string
		a, b any
	}{{"contacts", next.Contacts, current.Contacts}, {"contracts", next.Contracts, current.Contracts}, {"receipts", next.Receipts, current.Receipts}, {"externalReminder", next.ExternalReminder, current.ExternalReminder}} {
		if !reflect.DeepEqual(field.a, field.b) {
			changes = append(changes, field.name)
		}
	}
	return changes, nil
}
