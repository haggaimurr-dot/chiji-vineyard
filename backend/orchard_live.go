package main

import (
	"errors"
	"net/url"
	"strings"
)

type OrchardLiveConfig struct {
	Orchard OrchardLiveChannel `json:"orchard"`
	Stall   OrchardLiveChannel `json:"stall"`
}
type OrchardLiveChannel struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url"`
	Hours   string `json:"hours"`
}

func validateOrchardLive(live *OrchardLiveConfig) error {
	if live == nil {
		return nil
	}
	for _, channel := range []OrchardLiveChannel{live.Orchard, live.Stall} {
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
