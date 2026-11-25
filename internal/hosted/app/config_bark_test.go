package app

import (
	"strings"
	"testing"
)

func TestBarkURLIsAnAbsoluteHTTPSURLOrLoopbackHTTP(t *testing.T) {
	for _, ok := range []string{"https://bark.example.com", "https://bark.example.com/prefix", "https://bark.example.com:8443", "http://127.0.0.1:8080",
		"http://[::1]:8080", "http://localhost:8080"} {
		if _, err := LoadConfig(envOf(with("YH_BARK_URL", ok))); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"bark.example.com", "ftp://bark.example.com", "http://bark.example.com", "https://", "https:///push",
		"https://user:PASS-FAKE@bark.example.com", "https://bark.example.com?key=DEVICE-KEY-FAKE", "https://bark.example.com#x", " ", "//bark.example.com"} {
		_, err := LoadConfig(envOf(with("YH_BARK_URL", bad)))
		if err == nil || !strings.HasPrefix(err.Error(), "YH_BARK_URL:") {
			t.Errorf("%q: err = %v, want a refusal naming YH_BARK_URL", bad, err)
			continue
		}
		for _, leak := range []string{"PASS-FAKE", "DEVICE-KEY-FAKE", "bark.example.com"} {
			if strings.Contains(err.Error(), leak) {
				t.Errorf("%q: the refusal %q carries %q", bad, err, leak)
			}
		}
	}
}

func TestDuetReplyPushDefaultsOnAndTakesOffAndOn(t *testing.T) {
	for _, c := range []struct {
		val  string
		want bool
	}{{"", true}, {"on", true}, {"off", false}} {
		cfg, err := LoadConfig(envOf(with("YH_DUET_REPLY_PUSH", c.val)))
		if err != nil || cfg.DuetReplyPush != c.want {
			t.Errorf("%q: DuetReplyPush=%v err=%v, want %v", c.val, cfg.DuetReplyPush, err, c.want)
		}
	}
	for _, bad := range []string{"yes", "OFF", "0"} {
		if _, err := LoadConfig(envOf(with("YH_DUET_REPLY_PUSH", bad))); err == nil || !strings.HasPrefix(err.Error(), "YH_DUET_REPLY_PUSH:") {
			t.Errorf("%q: err = %v, want a refusal naming the variable", bad, err)
		}
	}
}

// serve hands the switch to the bus.
func TestBusLimitsFollowTheDuetReplySwitch(t *testing.T) {
	for _, push := range []bool{true, false} {
		l := busLimits(Config{DuetReplyPush: push, LinkHosts: []string{"github.com"}, WalledSwarms: []string{"client"}}, &kicker{})
		if l.NoDuetReplyPush == push || len(l.LinkHosts) != 1 || len(l.WalledSwarms) != 1 || l.OnCommit == nil {
			t.Errorf("DuetReplyPush=%v: limits %+v", push, l)
		}
	}
}
