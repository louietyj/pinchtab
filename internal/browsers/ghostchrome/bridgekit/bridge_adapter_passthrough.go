package bridgekit

func (a *BridgeAdapter) FocusTab(tabID string) error {
	if chromeID, ok := a.proxy.ChromeTabID(tabID); ok {
		return a.BridgeAPI.FocusTab(chromeID)
	}
	return a.BridgeAPI.FocusTab(tabID)
}

func (a *BridgeAdapter) CloseTab(tabID string) error {
	if chromeID, ok := a.proxy.ChromeTabID(tabID); ok {
		err := a.BridgeAPI.CloseTab(chromeID)
		a.proxy.ReleaseTab(tabID)
		return err
	}
	if sb := a.StaticBrowser(); sb != nil && sb.CloseTab(tabID) {
		a.proxy.ReleaseTab(tabID)
		return nil
	}
	return a.BridgeAPI.CloseTab(tabID)
}

func (a *BridgeAdapter) AvailableActions() []string {
	return a.proxy.AvailableActions()
}

func (a *BridgeAdapter) FingerprintRotateActive(tabID string) bool {
	type getter interface {
		FingerprintRotateActive(string) bool
	}
	g, ok := a.BridgeAPI.(getter)
	if !ok {
		return false
	}
	if g.FingerprintRotateActive(tabID) {
		return true
	}
	if chromeID, mapped := a.proxy.ChromeTabID(tabID); mapped {
		return g.FingerprintRotateActive(chromeID)
	}
	return false
}
