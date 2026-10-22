package main

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

// Empty dates remain valid for a presale batch that has not been harvested.
func validateProductBatch(batch *Traceability) error {
	if batch == nil {
		return nil
	}
	if batch.Code != "" && (strings.TrimSpace(batch.Code) == "" || utf8.RuneCountInString(strings.TrimSpace(batch.Code)) > 120 || strings.TrimSpace(batch.Batch) == "") {
		return errors.New("批次溯源码须为 1–120 字，并关联批次号")
	}
	for _, value := range []string{batch.HarvestedAt, batch.PackedAt} {
		if value != "" {
			if _, err := time.Parse("2006-01-02", value); err != nil {
				return errors.New("采收和包装日期须为有效的 YYYY-MM-DD 日期")
			}
		}
	}
	if batch.PackedAt != "" && batch.HarvestedAt == "" {
		return errors.New("填写包装日期前请先填写采收日期")
	}
	if batch.PackedAt != "" && batch.PackedAt < batch.HarvestedAt {
		return errors.New("包装日期不能早于采收日期")
	}
	return nil
}

func validateGiftCustomization(gift *GiftCustomization) error {
	if gift == nil || !gift.Enabled {
		return nil
	}
	if gift.MinimumQuantity < 1 || gift.MinimumQuantity > 1000000 {
		return errors.New("礼盒定制起订量须为 1–1000000 的整数")
	}
	if strings.TrimSpace(gift.Description) == "" || utf8.RuneCountInString(gift.Description) > 1000 {
		return errors.New("请填写 1–1000 字礼盒定制说明")
	}
	if strings.TrimSpace(gift.LeadTime) == "" || utf8.RuneCountInString(gift.LeadTime) > 200 {
		return errors.New("请填写 1–200 字定制周期")
	}
	if len(gift.Options) > 20 {
		return errors.New("定制项目最多 20 项，每项为 1–80 字")
	}
	for _, option := range gift.Options {
		if strings.TrimSpace(option) == "" || utf8.RuneCountInString(option) > 80 {
			return errors.New("定制项目最多 20 项，每项为 1–80 字")
		}
	}
	return nil
}
