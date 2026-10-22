package main

import (
	"strings"
	"testing"
)

func TestProductCSVValidation(t *testing.T) {
	csv := productCSVHeader + "\nA,葡萄,副标题,family,spot,,https://example.com/grape.jpg,果园,家庭装,89.90,12,5,2kg\nA,葡萄,副标题,family,spot,,https://example.com/grape.jpg,果园,分享装,120,8,3,4kg\n"
	out := parseProductCSV("\ufeff" + csv)
	if len(out.Errors) > 0 || len(out.Products) != 1 || len(out.Products[0].SKUs) != 2 || out.Products[0].SKUs[0].Price != 8990 || out.Products[0].Status != "DRAFT" {
		t.Fatal(out)
	}
	for _, bad := range []string{strings.Replace(csv, "89.90", "89.999", 1), strings.Replace(csv, ",12,", ",-1,", 1), strings.Replace(csv, "分享装", "家庭装", 1), strings.Replace(csv, "spot,,", "presale,2026-02-30,", 1), productCSVHeader + "\n", strings.Replace(csv, "A,葡萄,副标题", `A,"未闭合`, 1), strings.Replace(csv, "120,8", "21474836.48,8", 1)} {
		if len(parseProductCSV(bad).Errors) == 0 {
			t.Fatal("invalid import accepted", bad)
		}
	}
	quoted := strings.Replace(csv, "葡萄,副标题", `葡萄,"含逗号,及""引号"""`, -1)
	if out = parseProductCSV(quoted); len(out.Errors) > 0 || out.Products[0].Subtitle != "含逗号,及\"引号\"" {
		t.Fatal(out)
	}
}
func TestManagedBatchDates(t *testing.T) {
	b := ProductBatch{ProductID: "p", Number: "B01", Orchard: "果园", Status: "ACTIVE", HarvestedAt: "2026-10-01", PackedAt: "2026-10-02", ExpiresAt: "2026-10-08"}
	if e := validateManagedBatch(b); e != nil {
		t.Fatal(e)
	}
	for _, change := range []func(*ProductBatch){func(b *ProductBatch) { b.PackedAt = "2026-09-30" }, func(b *ProductBatch) { b.ExpiresAt = "2026-02-30" }, func(b *ProductBatch) { b.ExpiresAt = "2026-10-01" }, func(b *ProductBatch) { b.Number = " " }, func(b *ProductBatch) { b.Status = "unknown" }} {
		invalid := b
		change(&invalid)
		if validateManagedBatch(invalid) == nil {
			t.Fatal(invalid)
		}
	}
}
