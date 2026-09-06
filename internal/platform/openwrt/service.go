package openwrt

import "github.com/design-maestro/fastlane/internal/backend/xray"

// NewXrayController returns an init.d backed Xray controller.
func NewXrayController() xray.InitdController {
	return xray.InitdController{ScriptPath: XrayServicePath()}
}
