package main

import (
	"encoding/json"
	"strings"
	"testing"
	"reflect"
)

func TestOrchardLiveConfig(t *testing.T) {
	for _, tc := range []struct {
		name    string
		channel OrchardLiveChannel
		invalid bool
	}{
		{"not connected", OrchardLiveChannel{}, false},
		{"enabled without stream", OrchardLiveChannel{Enabled: true}, true},
		{"HLS with signature", OrchardLiveChannel{Enabled: true, URL: "https://live.example.com/orchard.m3u8?sign=abc", Hours: "每日 08:00–18:00"}, false},
		{"HTTP", OrchardLiveChannel{URL: "http://live.example.com/stall.m3u8"}, true},
		{"camera private protocol", OrchardLiveChannel{URL: "rtsp://camera.local/live"}, true},
		{"recording", OrchardLiveChannel{URL: "https://live.example.com/record.mp4"}, true},
		{"missing host", OrchardLiveChannel{URL: "https:///stall.m3u8"}, true},
		{"credentials", OrchardLiveChannel{URL: "https://user:pass@live.example.com/stall.m3u8"}, true},
		{"fragment", OrchardLiveChannel{URL: "https://live.example.com/stall.m3u8#x"}, true},
		{"whitespace", OrchardLiveChannel{URL: "https://live.example.com/stall.m3u8?sign=a b"}, true},
		{"hours too long", OrchardLiveChannel{Hours: strings.Repeat("时", 101)}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := defaultHome()
			home.Live = &OrchardLiveConfig{Orchard: tc.channel}
			if err := validateHome(home); (err != nil) != tc.invalid {
				t.Fatalf("validation: %v", err)
			}
			home.Live = &OrchardLiveConfig{Stall: tc.channel}
			if err := validateHome(home); (err != nil) != tc.invalid {
				t.Fatalf("stall validation: %v", err)
			}
		})
	}
	home := defaultHome()
	home.Live = &OrchardLiveConfig{Orchard: OrchardLiveChannel{Enabled: true, URL: "https://live.example.com/orchard.m3u8"}, Stall: OrchardLiveChannel{Hours: "休息中"}}
	raw, err := json.Marshal(home)
	if err != nil {
		t.Fatal(err)
	}
	var read HomeConfig
	if err = json.Unmarshal(raw, &read); err != nil {
		t.Fatal(err)
	}
	if read.Live == nil || !reflect.DeepEqual(read.Live, home.Live) {
		t.Fatal("two independent camera configurations must survive persistence")
	}
}
