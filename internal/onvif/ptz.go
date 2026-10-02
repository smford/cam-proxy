package onvif

import (
	"context"
	"fmt"
)

// PTZMoveCommand specifies velocity vectors for continuous move.
type PTZMoveCommand struct {
	ProfileToken string  `json:"profile_token"`
	Pan          float64 `json:"pan"`  // -1.0 to 1.0
	Tilt         float64 `json:"tilt"` // -1.0 to 1.0
	Zoom         float64 `json:"zoom"` // -1.0 to 1.0
}

// PTZPresetCommand specifies a preset token to recall.
type PTZPresetCommand struct {
	ProfileToken string `json:"profile_token"`
	PresetToken  string `json:"preset_token"`
}

// ContinuousMove sends a ContinuousMove SOAP request to the ONVIF PTZ service.
func (d *Device) ContinuousMove(ctx context.Context, cmd PTZMoveCommand) error {
	ptzURL := d.GetPTZURL(ctx)

	body := fmt.Sprintf(`
    <tptz:ContinuousMove>
      <tptz:ProfileToken>%s</tptz:ProfileToken>
      <tptz:Velocity>
        <tt:PanTilt x="%.2f" y="%.2f" space="http://www.onvif.org/ver10/tptz/PanTiltSpaces/VelocityGenericSpace"/>
        <tt:Zoom x="%.2f" space="http://www.onvif.org/ver10/tptz/ZoomSpaces/VelocityGenericSpace"/>
      </tptz:Velocity>
    </tptz:ContinuousMove>`, cmd.ProfileToken, cmd.Pan, cmd.Tilt, cmd.Zoom)

	_, err := d.SendSOAP(ctx, ptzURL, body)
	if err != nil {
		return fmt.Errorf("PTZ continuous move failed: %w", err)
	}
	return nil
}

// Stop sends a Stop SOAP request to halt ongoing PTZ motion.
func (d *Device) Stop(ctx context.Context, profileToken string) error {
	ptzURL := d.GetPTZURL(ctx)

	body := fmt.Sprintf(`
    <tptz:Stop>
      <tptz:ProfileToken>%s</tptz:ProfileToken>
      <tptz:PanTilt>true</tptz:PanTilt>
      <tptz:Zoom>true</tptz:Zoom>
    </tptz:Stop>`, profileToken)

	_, err := d.SendSOAP(ctx, ptzURL, body)
	if err != nil {
		return fmt.Errorf("PTZ stop failed: %w", err)
	}
	return nil
}

// GotoPreset moves the camera to a predefined ONVIF PTZ preset.
func (d *Device) GotoPreset(ctx context.Context, cmd PTZPresetCommand) error {
	ptzURL := d.GetPTZURL(ctx)

	body := fmt.Sprintf(`
    <tptz:GotoPreset>
      <tptz:ProfileToken>%s</tptz:ProfileToken>
      <tptz:PresetToken>%s</tptz:PresetToken>
    </tptz:GotoPreset>`, cmd.ProfileToken, cmd.PresetToken)

	_, err := d.SendSOAP(ctx, ptzURL, body)
	if err != nil {
		return fmt.Errorf("PTZ goto preset failed: %w", err)
	}
	return nil
}
