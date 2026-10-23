// v1.4.3: BLE 网关协议——耳机/眼镜/指环接入模拟。
package edge

import "fmt"

type DeviceType string

const (
	DeviceEarbud  DeviceType = "earbud"
	DeviceGlasses DeviceType = "glasses"
	DeviceRing    DeviceType = "ring"
)

type Device struct {
	ID       string
	Type     DeviceType
	Battery  int // 0-100
	Connected bool
}

// BLEGateway 模拟 BLE 设备接入。
type BLEGateway struct {
	devices map[string]*Device
}

func NewBLEGateway() *BLEGateway {
	return &BLEGateway{devices: make(map[string]*Device)}
}

func (g *BLEGateway) Connect(id string, t DeviceType) (*Device, error) {
	if _, ok := g.devices[id]; ok {
		return nil, fmt.Errorf("device %s already connected", id)
	}
	d := &Device{ID: id, Type: t, Battery: 80, Connected: true}
	g.devices[id] = d
	return d, nil
}

func (g *BLEGateway) Disconnect(id string) {
	if d, ok := g.devices[id]; ok {
		d.Connected = false
		delete(g.devices, id)
	}
}

func (g *BLEGateway) List() []*Device {
	out := []*Device{}
	for _, d := range g.devices {
		out = append(out, d)
	}
	return out
}
