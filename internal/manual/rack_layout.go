package manual

import (
	"fmt"
	"time"

	"github.com/blalange/auto-netviz/internal/model"
)

// RackPlacement holds the computed bottom-to-top U span for a device in a rack.
type RackPlacement struct {
	DeviceName string
	StartU     int
	EndU       int
	UHeight    int
	Face       string
}

// RackPlan contains computed placements and any validation issues for one rack.
type RackPlan struct {
	Positions map[string]RackPlacement
	UsedU     int
	Issues    []model.Issue
}

// PlanRackLayout computes top-to-bottom U positions for a rack's declarative layout,
// mirroring netbox-inventory-toolkit's unit accounting and validation checks.
func PlanRackLayout(rack RackSpec, devices map[string]DeviceSpec, deviceTypes map[string]DeviceTypeSpec) RackPlan {
	plan := RackPlan{
		Positions: make(map[string]RackPlacement),
	}
	uHeight := rack.UHeight
	if uHeight <= 0 {
		uHeight = 42
	}
	if len(rack.Layout) == 0 {
		return plan
	}

	now := time.Now().UTC()
	cursor := uHeight
	seen := make(map[string]struct{})

	for idx, entry := range rack.Layout {
		gap := entry.Gap
		if gap == 0 && entry.Empty > 0 {
			gap = entry.Empty
		}
		if gap > 0 {
			cursor -= gap
			if cursor < 0 {
				plan.Issues = append(plan.Issues, model.Issue{
					ID:         fmt.Sprintf("rack-overflow:%s:%s:%d", rack.Site, rack.Name, idx),
					Severity:   "error",
					Kind:       "rack_layout",
					EntityKind: "rack",
					EntityName: rack.Name,
					Field:      "layout",
					Message:    fmt.Sprintf("Rack %q layout exceeds its %dU height at entry %d.", rack.Name, uHeight, idx),
					DetectedAt: now,
				})
			}
			continue
		}

		name := entry.Device
		if name == "" {
			plan.Issues = append(plan.Issues, model.Issue{
				ID:         fmt.Sprintf("rack-entry:%s:%s:%d", rack.Site, rack.Name, idx),
				Severity:   "error",
				Kind:       "rack_layout",
				EntityKind: "rack",
				EntityName: rack.Name,
				Field:      "layout",
				Message:    fmt.Sprintf("Rack %q layout entry %d must define either device or gap.", rack.Name, idx),
				DetectedAt: now,
			})
			continue
		}
		if _, dup := seen[name]; dup {
			plan.Issues = append(plan.Issues, model.Issue{
				ID:         fmt.Sprintf("rack-dup:%s:%s:%s", rack.Site, rack.Name, name),
				Severity:   "error",
				Kind:       "rack_layout",
				EntityKind: "rack",
				EntityName: rack.Name,
				Field:      "layout",
				Message:    fmt.Sprintf("Device %q appears more than once in rack %q.", name, rack.Name),
				DetectedAt: now,
			})
			continue
		}
		seen[name] = struct{}{}

		dev, ok := devices[name]
		if !ok {
			plan.Issues = append(plan.Issues, model.Issue{
				ID:         fmt.Sprintf("rack-unknown-dev:%s:%s:%s", rack.Site, rack.Name, name),
				Severity:   "error",
				Kind:       "rack_layout",
				EntityKind: "rack",
				EntityName: rack.Name,
				Field:      "layout",
				Message:    fmt.Sprintf("Rack %q references unknown device %q.", rack.Name, name),
				DetectedAt: now,
			})
			continue
		}

		height := 1
		if dt, dtOK := deviceTypes[dev.DeviceType]; dtOK && dt.UHeight > 0 {
			height = dt.UHeight
		} else if dev.DeviceType != "" && !dtOK {
			plan.Issues = append(plan.Issues, model.Issue{
				ID:         fmt.Sprintf("rack-unknown-dt:%s:%s:%s", rack.Site, rack.Name, dev.DeviceType),
				Severity:   "warning",
				Kind:       "rack_layout",
				EntityKind: "device",
				EntityName: name,
				Field:      "device_type",
				Message:    fmt.Sprintf("Device %q in rack %q uses undefined device_type %q; assuming 1U.", name, rack.Name, dev.DeviceType),
				DetectedAt: now,
			})
		}

		pos := cursor - height + 1
		if pos < 1 {
			plan.Issues = append(plan.Issues, model.Issue{
				ID:         fmt.Sprintf("rack-no-fit:%s:%s:%s", rack.Site, rack.Name, name),
				Severity:   "error",
				Kind:       "rack_layout",
				EntityKind: "rack",
				EntityName: rack.Name,
				Field:      "layout",
				Message:    fmt.Sprintf("Device %q (%dU) does not fit in rack %q.", name, height, rack.Name),
				DetectedAt: now,
			})
			continue
		}

		face := entry.Face
		if face == "" {
			face = dev.Face
		}
		if face == "" {
			face = "front"
		}
		end := pos + height - 1
		plan.Positions[name] = RackPlacement{
			DeviceName: name,
			StartU:     pos,
			EndU:       end,
			UHeight:    height,
			Face:       face,
		}
		plan.UsedU += height
		cursor = pos - 1
	}

	if cursor != 0 {
		used := uHeight - cursor
		severity := "info"
		var msg string
		if used < uHeight {
			msg = fmt.Sprintf("Rack %q layout specifies %d of %dU (%dU remaining).", rack.Name, used, uHeight, uHeight-used)
		} else {
			severity = "error"
			msg = fmt.Sprintf("Rack %q layout uses %d of %dU (overflows by %dU).", rack.Name, used, uHeight, used-uHeight)
		}
		plan.Issues = append(plan.Issues, model.Issue{
			ID:         fmt.Sprintf("rack-incomplete:%s:%s", rack.Site, rack.Name),
			Severity:   severity,
			Kind:       "rack_layout",
			EntityKind: "rack",
			EntityName: rack.Name,
			Field:      "layout",
			Message:    msg,
			DetectedAt: now,
		})
	}

	return plan
}
