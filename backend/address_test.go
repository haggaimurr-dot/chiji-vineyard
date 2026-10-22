package main

import "testing"

func TestAddressBookLifecycle(t *testing.T) {
	list, err := decodeAddresses([]byte(`{"name":"张三","phone":"13800000000","province":"浙江省","city":"杭州市","detail":"1号"}`))
	if err != nil || len(list) != 1 || !list[0].IsDefault || list[0].ID != "legacy" {
		t.Fatalf("legacy migration: %v %v", list, err)
	}
	a := list[0]
	a.ID = ""
	a.IsDefault = false
	a.Detail = "2号"
	list, saved, err := changeAddresses(list, "address.save", a, "")
	if err != nil || len(list) != 2 || saved.IsDefault {
		t.Fatalf("add: %v %v", list, err)
	}
	id := saved.ID
	list, _, err = changeAddresses(list, "address.default", Address{}, id)
	if err != nil || list[0].IsDefault || !list[1].IsDefault {
		t.Fatal("default not exclusive")
	}
	a = list[1]
	a.Detail = "3号"
	a.IsDefault = false
	list, _, err = changeAddresses(list, "address.save", a, "")
	if err != nil || !list[1].IsDefault || list[1].Detail != "3号" {
		t.Fatal("editing loses default")
	}
	if _, _, err = changeAddresses(list, "address.delete", Address{}, "other-user-id"); err == nil {
		t.Fatal("unknown ID accepted")
	}
	list, _, err = changeAddresses(list, "address.delete", Address{}, id)
	if err != nil || len(list) != 1 || !list[0].IsDefault {
		t.Fatal("default deletion did not promote remaining address")
	}
	list, _, err = changeAddresses(list, "address.delete", Address{}, "legacy")
	if err != nil || len(list) != 0 || jsonValue(list) != "[]" {
		t.Fatal("last deletion")
	}
	a.ID = ""
	a.Phone = "invalid"
	if _, _, err = changeAddresses(list, "address.save", a, ""); err == nil {
		t.Fatal("invalid phone accepted")
	}
}
