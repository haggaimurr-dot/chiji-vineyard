package main

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"
)

func validateCustomerProfile(p *CustomerProfile) error {
	for _, field := range []struct {
		value *string
		limit int
	}{{&p.Email, 200}, {&p.Address, 200}, {&p.Industry, 200}, {&p.Notes, 2000}} {
		*field.value = strings.TrimSpace(*field.value)
		if utf8.RuneCountInString(*field.value) > field.limit {
			return errors.New("客户档案内容过长")
		}
	}
	if p.Email != "" && !regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`).MatchString(p.Email) {
		return errors.New("客户邮箱格式无效")
	}
	return nil
}

type LeadFunnelStage struct {
	Status string `json:"status"`
	Count  int    `json:"count"`
	Amount int64  `json:"amount"`
}
type LeadFunnel struct {
	Stages       []LeadFunnelStage `json:"stages"`
	Total        int               `json:"total"`
	Amount       int64             `json:"amount"`
	ActiveAmount int64             `json:"activeAmount"`
	WonAmount    int64             `json:"wonAmount"`
	WinRate      float64           `json:"winRate"`
}

func (s *Store) leadFunnel(ctx context.Context, search LeadSearch) (LeadFunnel, error) {
	// Status selection only affects the table; the funnel compares all stages.
	search.Status = ""
	d, err := normalizeLeadSearch(search)
	result := LeadFunnel{Stages: []LeadFunnelStage{{Status: "NEW"}, {Status: "CONTACTED"}, {Status: "QUALIFIED"}, {Status: "WON"}, {Status: "LOST"}}}
	if err != nil {
		return result, err
	}
	where, args := leadSearchWhere(d)
	rows, err := s.db.QueryContext(ctx, "SELECT status,COUNT(*),COALESCE(SUM(COALESCE(JSON_EXTRACT(payload,'$.opportunityAmount'),0)),0) FROM partner_leads"+where+" GROUP BY status", args...)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var count int
		var amount int64
		if err = rows.Scan(&status, &count, &amount); err != nil {
			return result, err
		}
		for i := range result.Stages {
			if result.Stages[i].Status == status {
				result.Stages[i].Count = count
				result.Stages[i].Amount = amount
			}
		}
		result.Total += count
		result.Amount += amount
		if status == "WON" {
			result.WonAmount += amount
		} else if status != "LOST" {
			result.ActiveAmount += amount
		}
	}
	if result.Total > 0 {
		result.WinRate = float64(result.Stages[3].Count) / float64(result.Total)
	}
	return result, rows.Err()
}
