package model

// Device 是设备管理接口对外的块设备 / 分区视图。
type Device struct {
	Device     string `json:"device"`      // 分区块设备路径，如 /dev/sdc1
	ID         string `json:"id"`          // 虚拟命名空间挂载点名（/drive/<id>）
	Name       string `json:"name"`        // 展示名
	Label      string `json:"label"`       // 卷标（可空）
	FSType     string `json:"fstype"`      // 文件系统类型（可空）
	Size       int64  `json:"size"`        // 容量字节
	Mounted    bool   `json:"mounted"`     // 是否已挂载
	Mountpoint string `json:"mountpoint"`  // OS 挂载目录（仅 mounted 时有值）
	DrivePath  string `json:"drive_path"`  // 虚拟浏览路径 /drive/<id>（前端「进入」用）
	Removable  bool   `json:"removable"`   // 是否可移动设备（USB / 热插拔）
	Readonly   bool   `json:"readonly"`    // 是否只读
	System     bool   `json:"system"`      // 是否为系统关键挂载（根 / 引导分区），前端禁用卸载
}

// DeviceListResult 是 GET /api/devices 的返回体。
type DeviceListResult struct {
	Supported bool     `json:"supported"` // 设备管理是否可用
	Devices   []Device `json:"devices"`
}
