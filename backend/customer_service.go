package main

import (
	"errors"
	"regexp"
	"strings"
)

type CustomerService struct {
	Enabled      bool   `json:"enabled"`
	Wechat       bool   `json:"wechat"`
	Phone        string `json:"phone"`
	Hours        string `json:"hours"`
	ResponseTime string `json:"responseTime"`
}

var servicePhone = regexp.MustCompile(`^(1[0-9]{10}|0[0-9]{2,3}-?[0-9]{7,8}|400-?[0-9]{3}-?[0-9]{4})$`)

func validateCustomerService(c CustomerService) error {
	if len([]rune(c.Hours)) > 100 || len([]rune(c.ResponseTime)) > 100 || (c.Phone != "" && !servicePhone.MatchString(c.Phone)) {
		return errors.New("客服号码格式无效，服务时间和回复说明最多100字")
	}
	if c.Enabled && (strings.TrimSpace(c.Hours) == "" || (!c.Wechat && c.Phone == "")) {
		return errors.New("启用客服前请填写服务时间，并开通微信客服或填写电话")
	}
	return nil
}
