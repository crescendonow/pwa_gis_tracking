package services

import "testing"

func TestNewLayersAreRegisteredForEveryCoreGISSurface(t *testing.T) {
	expected := map[string]string{
		"dma_boundary": "ขอบเขต DMA",
		"step_test":    "จุดทดสอบ Step Test",
		"flow_meter":   "มาตรวัดอัตราการไหล",
	}

	registered := make(map[string]bool)
	for _, name := range GetAllLayerNames() {
		registered[name] = true
	}

	for name, displayName := range expected {
		if !registered[name] {
			t.Errorf("GetAllLayerNames() does not include %q", name)
		}
		if _, ok := LayerConfigs[name]; !ok {
			t.Errorf("LayerConfigs does not include %q", name)
		}
		if got := GetLayerDisplayName(name); got != displayName {
			t.Errorf("GetLayerDisplayName(%q) = %q, want %q", name, got, displayName)
		}
	}
}
