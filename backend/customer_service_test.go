package main

import "testing"

func TestCustomerServiceValidation(t *testing.T) {
	for _, c := range []CustomerService{
		{},
		{Enabled: true, Wechat: true, Hours: "09:00–18:00"},
		{Enabled: true, Phone: "400-000-0000", Hours: "工作日"},
		{Enabled: true, Phone: "0571-12345678", Hours: "工作日"},
	} {
		if err := validateCustomerService(c); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []CustomerService{
		{Enabled: true, Wechat: true},
		{Enabled: true, Hours: "工作日"},
		{Phone: "javascript:alert(1)"},
		{Phone: "123"},
	} {
		if validateCustomerService(c) == nil {
			t.Fatalf("invalid config accepted: %+v", c)
		}
	}
}
