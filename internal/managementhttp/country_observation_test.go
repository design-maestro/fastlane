package managementhttp

import (
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/design-maestro/fastlane/internal/app"
	"github.com/design-maestro/fastlane/internal/domain"
	"github.com/design-maestro/fastlane/internal/store"
)

func TestPanelKeepsMeasuredCountryIndependentOfHealth(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is unavailable")
	}
	script := `
const fs=require('fs'), vm=require('vm'), assert=require('assert/strict');
const source=fs.readFileSync('web/panel.js','utf8');
const sandbox={snapshot:{status:{state:{health:{n:{healthy:true,last_latency:'40ms',last_checked_at:'2026-09-18T07:00:00Z',country_code:'SE',egress_ip:'192.0.2.2'}}}}},probeResults:{},awgs:[]};
vm.createContext(sandbox);
vm.runInContext(source.slice(source.indexOf('function countryCode('), source.indexOf('function connectNode(')),sandbox);
const n={id:'n',subscription_id:'s',name:'Netherlands'};
assert.equal(sandbox.countryCode(n),'SE');
sandbox.probeResults['s/n']={success:false,checked_at:'2026-09-18T07:01:00Z'};
assert.equal(sandbox.countryCode(n),'SE');
assert.equal(sandbox.nodeHealth(n).healthy,false);
sandbox.probeResults['s/n']={success:true,checked_at:'2026-09-18T07:02:00Z',latency_ms:55,country_code:'FI'};
assert.equal(sandbox.countryCode(n),'FI');
sandbox.snapshot.status.state.health.n={healthy:false,last_checked_at:'2026-09-18T07:03:00Z'};
assert.equal(sandbox.countryCode(n),'FI');
assert.equal(sandbox.nodeHealth(n).healthy,false);
assert.equal(sandbox.countryCode({id:'unknown',subscription_id:'s',name:'🇳🇱 Netherlands'}),'');
sandbox.awgs=[{id:'awg',last_probe:{success:true,country_code:'PT',latency_ms:50}}];
assert.equal(sandbox.countryCode({id:'awg',kind:'awg'}),'PT');
`
	if out, err := exec.Command(node, "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("panel country contract: %v\n%s", err, out)
	}
}

func TestVPNCountryBrowser(t *testing.T) {
	if os.Getenv("FASTLANE_PANEL_BROWSER") != "1" {
		t.Skip("set FASTLANE_PANEL_BROWSER=1")
	}
	fs := store.NewFileStore(t.TempDir())
	s := app.NewService(app.Dependencies{Store: fs, AWGStore: fs})
	sub, err := s.AddSubscription(context.Background(), app.AddSubscriptionRequest{Raw: "vless://11111111-1111-4111-8111-111111111111@vpn.example:443?security=tls#Netherlands"})
	if err != nil {
		t.Fatal(err)
	}
	state := domain.DefaultRuntimeState()
	state.Health = map[string]domain.NodeHealth{sub.Nodes[0].ID: {NodeID: sub.Nodes[0].ID, CountryCode: "SE", Healthy: true, LastLatency: domain.NewDuration(40 * time.Millisecond), LastCheckedAt: time.Now().UTC()}}
	if err := fs.SaveState(state); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(mustHandler(t, context.Background(), s, HandlerConfig{AccessToken: testAccessToken}))
	defer server.Close()
	alloc, cancel := chromedp.NewExecAllocator(context.Background(), chromedp.DefaultExecAllocatorOptions[:]...)
	defer cancel()
	ctx, cancel := chromedp.NewContext(alloc)
	defer cancel()
	ctx, cancel = context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	if err := chromedp.Run(ctx, chromedp.Navigate(server.URL), chromedp.WaitVisible("#login"), chromedp.SendKeys("#login-form input", testAccessToken), chromedp.Click("#login-form button"), chromedp.WaitVisible("#server-list tbody tr")); err != nil {
		t.Fatal(err)
	}
	for _, width := range []int64{390, 1440} {
		var valid bool
		if err := chromedp.Run(ctx, chromedp.EmulateViewport(width, 1000), chromedp.Evaluate(`document.querySelector('.fl-server-mark').textContent==='🇸🇪' && document.querySelector('.fl-server-name').textContent==='Netherlands' && document.documentElement.scrollWidth<=innerWidth`, &valid)); err != nil || !valid {
			t.Fatalf("measured country at %d: %v", width, err)
		}
	}
	observation := state.Health[sub.Nodes[0].ID]
	observation.Healthy, observation.LastCheckedAt = false, time.Now().UTC().Add(time.Minute)
	state.Health[sub.Nodes[0].ID] = observation
	if err := fs.SaveState(state); err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Run(ctx, chromedp.Reload(), chromedp.WaitVisible("#server-list tbody tr")); err != nil {
		t.Fatal(err)
	}
	var valid bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('.fl-server-mark').textContent==='🇸🇪' && document.querySelector('#server-list').textContent.includes('Недоступен')`, &valid)); err != nil || !valid {
		t.Fatalf("failed GET lost country or retained healthy status: %v", err)
	}
}
