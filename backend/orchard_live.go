package main

import (
	"errors"
	"time"
	"net/url"
	"strings"
)

type OrchardLiveConfig struct {
	Orchard OrchardLiveChannel `json:"orchard"`
	Stall   OrchardLiveChannel `json:"stall"`
}
type OrchardLiveChannel struct {
	Enabled    bool     `json:"enabled"`
	URL        string   `json:"url"`
	Hours      string   `json:"hours"`
	NoticeAt   string   `json:"noticeAt,omitempty"`
	NoticeText string   `json:"noticeText,omitempty"`
	ProductIDs []string `json:"productIds,omitempty"`
}

func validateOrchardLive(live *OrchardLiveConfig) error {
	if live == nil {
		return nil
	}
	for _, channel := range []OrchardLiveChannel{live.Orchard, live.Stall} {
		seen := map[string]bool{}
		for _, id := range channel.ProductIDs {
			if id == "" || seen[id] {
				return errors.New("直播挂品存在空值或重复")
			}
			seen[id] = true
		}
		if len(channel.ProductIDs) > 10 {
			return errors.New("单个机位挂品最多10件")
		}
	}
	for _, channel := range []OrchardLiveChannel{live.Orchard, live.Stall} {
		if len([]rune(channel.NoticeText)) > 100 {
			return errors.New("直播预告文案最多100字")
		}
		if channel.NoticeAt != "" {
			if _, err := time.Parse(time.RFC3339, channel.NoticeAt); err != nil {
				return errors.New("直播预告时间格式无效")
			}
		}
		if len([]rune(channel.Hours)) > 100 {
			return errors.New("直播时间说明最多100字")
		}
		if channel.URL == "" {
			if channel.Enabled {
				return errors.New("开启机位前请填写直播播放地址")
			}
			continue
		}
		parsed, err := url.Parse(channel.URL)
		if err != nil || len(channel.URL) > 2000 || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" || strings.ContainsAny(channel.URL, " \t\r\n") || !strings.HasSuffix(strings.ToLower(parsed.Path), ".m3u8") {
			return errors.New("直播播放地址须为有效的 HTTPS HLS（.m3u8）地址")
		}
	}
	return nil
}
