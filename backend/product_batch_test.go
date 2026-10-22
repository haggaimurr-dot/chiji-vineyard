package main

import (
	"encoding/json"
	"testing"
)

func TestProductBatchDates(t *testing.T) {
	for _, test := range []struct {
		name  string
		batch *Traceability
		valid bool
	}{
		{"legacy", nil, true},
		{"trace code", &Traceability{Batch: "BATCH-1", Code: "TRACE-1"}, true},
		{"code without batch", &Traceability{Code: "TRACE-1"}, false},
		{"unharvested presale", &Traceability{Demo: true}, true},
		{"harvested", &Traceability{HarvestedAt: "2026-10-03"}, true},
		{"packed", &Traceability{HarvestedAt: "2026-10-03", PackedAt: "2026-10-04"}, true},
		{"impossible date", &Traceability{HarvestedAt: "2026-02-30"}, false},
		{"missing harvest", &Traceability{PackedAt: "2026-10-04"}, false},
		{"packing before harvest", &Traceability{HarvestedAt: "2026-10-04", PackedAt: "2026-10-03"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := validateProductBatch(test.batch); (err == nil) != test.valid {
				t.Fatalf("validation: %v", err)
			}
		})
	}
}

func TestProductContentJSONRoundTrip(t *testing.T) {
	// Product payloads use this JSON representation for storage and detail responses.
	raw := []byte(`{"variety":"阳光玫瑰","storage":"冷藏保存（示例）","netWeightNote":"不含外箱（示例）","imageNote":"商品示意图","traceability":{"orchard":"示例果园","batch":"DEMO-1003","harvestedAt":"2026-10-03","packedAt":"2026-10-04","packing":"内托（示例）","steps":[],"demo":true}}`)
	var product Product
	if err := json.Unmarshal(raw, &product); err != nil {
		t.Fatal(err)
	}
	product.Traceability.Code = "DEMO-TRACE-1003"
	product.GiftCustomization = &GiftCustomization{Enabled: true, Description: "祝福卡定制", Options: []string{"祝福卡"}, MinimumQuantity: 20, LeadTime: "7 天"}
	encoded, err := json.Marshal(product)
	if err != nil {
		t.Fatal(err)
	}
	var restored Product
	if err = json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.Traceability.Code != product.Traceability.Code || restored.GiftCustomization == nil || restored.GiftCustomization.MinimumQuantity != 20 || restored.GiftCustomization.Options[0] != "祝福卡" {
		t.Fatalf("customization lost: %s", encoded)
	}
	if restored.Variety != "阳光玫瑰" || restored.Storage != product.Storage || restored.NetWeightNote != product.NetWeightNote || restored.ImageNote != "商品示意图" || restored.Traceability == nil || restored.Traceability.PackedAt != "2026-10-04" || !restored.Traceability.Demo {
		t.Fatalf("content lost: %s", encoded)
	}
}

func TestGiftCustomizationValidation(t *testing.T) {
	if err := validateGiftCustomization(nil); err != nil {
		t.Fatal(err)
	}
	if err := validateGiftCustomization(&GiftCustomization{}); err != nil {
		t.Fatal(err)
	}
	valid := GiftCustomization{Enabled: true, Description: "定制说明", Options: []string{"祝福卡"}, MinimumQuantity: 20, LeadTime: "7 天"}
	if err := validateGiftCustomization(&valid); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*GiftCustomization){
		func(g *GiftCustomization) { g.MinimumQuantity = 0 },
		func(g *GiftCustomization) { g.MinimumQuantity = 1000001 },
		func(g *GiftCustomization) { g.Description = "" },
		func(g *GiftCustomization) { g.LeadTime = "" },
		func(g *GiftCustomization) { g.Options = []string{" "} },
	} {
		gift := valid
		change(&gift)
		if validateGiftCustomization(&gift) == nil {
			t.Fatalf("accepted invalid customization: %+v", gift)
		}
	}
}
