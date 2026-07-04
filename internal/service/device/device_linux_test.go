//go:build linux

package device

import (
	"errors"
	"testing"
)

func TestParseLsblk(t *testing.T) {
	// 覆盖真实场景：SATA 系统盘（含 /boot、/ 系统分区）、USB U 盘（tran=usb）、
	// snap 的 loop 虚拟盘、swap 分区、无文件系统裸分区，以及 size/rm 的字符串/数字表示。
	sample := []byte(`{
      "blockdevices": [
        {
          "name":"sda","path":"/dev/sda","type":"disk","size":500107862016,
          "fstype":null,"label":null,"mountpoint":null,"rm":false,"ro":false,
          "hotplug":false,"tran":"sata",
          "children":[
            {"name":"sda1","path":"/dev/sda1","type":"part","size":524288000,
             "fstype":"vfat","label":"BOOT","mountpoint":"/boot","rm":false,"ro":false},
            {"name":"sda2","path":"/dev/sda2","type":"part","size":499582500864,
             "fstype":"ext4","label":"root","mountpoint":"/","rm":false,"ro":false},
            {"name":"sda3","path":"/dev/sda3","type":"part","size":2147483648,
             "fstype":"swap","label":null,"mountpoint":"[SWAP]","rm":false,"ro":false}
          ]
        },
        {
          "name":"sdc","path":"/dev/sdc","type":"disk","size":"61530439680",
          "fstype":null,"label":null,"mountpoint":null,"rm":"1","ro":"0",
          "hotplug":"1","tran":"usb",
          "children":[
            {"name":"sdc1","path":"/dev/sdc1","type":"part","size":"61530000000",
             "fstype":"exfat","label":"KINGSTON","mountpoint":null,"rm":1,"ro":0},
            {"name":"sdc2","path":"/dev/sdc2","type":"part","size":1048576,
             "fstype":null,"label":null,"mountpoint":null,"rm":1,"ro":0}
          ]
        },
        {
          "name":"loop0","path":"/dev/loop0","type":"loop","size":14155776,
          "fstype":"squashfs","label":null,"mountpoint":"/snap/core/1","rm":false,"ro":true,
          "hotplug":false,"tran":null
        }
      ]
    }`)

	devices, err := parseLsblk(sample)
	if err != nil {
		t.Fatalf("parseLsblk: %v", err)
	}

	byDev := map[string]Device{}
	for _, d := range devices {
		byDev[d.Device] = d
	}

	// 系统盘分区应被收录，且标记为 System；非可移动（sata）。
	sda1, ok := byDev["/dev/sda1"]
	if !ok {
		t.Fatal("expected /dev/sda1 to be parsed")
	}
	if !sda1.Mounted || sda1.Mountpoint != "/boot" {
		t.Errorf("sda1 should be mounted at /boot: %+v", sda1)
	}
	if !sda1.System {
		t.Errorf("sda1 (/boot) should be marked system: %+v", sda1)
	}
	if sda1.Removable {
		t.Errorf("sda1 on sata disk should not be removable: %+v", sda1)
	}
	if sda2, ok := byDev["/dev/sda2"]; !ok || !sda2.System {
		t.Errorf("sda2 (/) should be parsed and marked system: %+v", sda2)
	}

	// swap 分区不可浏览，应被过滤。
	if _, ok := byDev["/dev/sda3"]; ok {
		t.Errorf("swap partition /dev/sda3 should be filtered out")
	}

	// USB U 盘分区：tran=usb → removable；size/rm 字符串正确解析；未挂载。
	sdc1, ok := byDev["/dev/sdc1"]
	if !ok {
		t.Fatal("expected /dev/sdc1 to be parsed")
	}
	if sdc1.Size != 61530000000 {
		t.Errorf("sdc1 size mismatch: %d", sdc1.Size)
	}
	if !sdc1.Removable {
		t.Errorf("sdc1 on usb disk should be removable: %+v", sdc1)
	}
	if sdc1.System {
		t.Errorf("sdc1 should not be system: %+v", sdc1)
	}

	// sdc2 无文件系统（EFI 空分区场景外）→ 过滤。
	if _, ok := byDev["/dev/sdc2"]; ok {
		t.Errorf("partition without fstype /dev/sdc2 should be filtered out")
	}

	// loop 设备（snap squashfs）一律过滤。
	if _, ok := byDev["/dev/loop0"]; ok {
		t.Errorf("loop device /dev/loop0 should be filtered out")
	}

	// 有子分区的整盘不作为条目收录。
	if _, ok := byDev["/dev/sda"]; ok {
		t.Errorf("whole disk /dev/sda with children should not be listed")
	}
	if _, ok := byDev["/dev/sdc"]; ok {
		t.Errorf("whole disk /dev/sdc with children should not be listed")
	}
}

func TestParseLsblkWholeDiskUsb(t *testing.T) {
	// 无分区表直接格式化的 U 盘（整盘即文件系统），tran=usb → 收录且 removable。
	sample := []byte(`{
      "blockdevices": [
        {"name":"sdb","path":"/dev/sdb","type":"disk","size":16000000000,
         "fstype":"vfat","label":"USBKEY","mountpoint":null,"rm":true,"ro":false,
         "hotplug":true,"tran":"usb"}
      ]
    }`)
	devices, err := parseLsblk(sample)
	if err != nil {
		t.Fatalf("parseLsblk: %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(devices))
	}
	d := devices[0]
	if d.Device != "/dev/sdb" || !d.Removable || d.Label != "USBKEY" {
		t.Errorf("whole-disk usb key parsed wrong: %+v", d)
	}
}

func TestParseLsblkEmpty(t *testing.T) {
	devices, err := parseLsblk([]byte(`{"blockdevices":[]}`))
	if err != nil {
		t.Fatalf("parseLsblk empty: %v", err)
	}
	if len(devices) != 0 {
		t.Errorf("expected no devices, got %d", len(devices))
	}
}

func TestParseMountpoint(t *testing.T) {
	cases := map[string]string{
		"Mounted /dev/sdc1 at /run/media/user/KINGSTON.":  "/run/media/user/KINGSTON",
		"Mounted /dev/sdc1 at /run/media/user/My Disk":    "/run/media/user/My Disk",
		"Mounted /dev/sda1 at /mnt/usb\n":                 "/mnt/usb",
		"":                                                "",
		"some unexpected output":                          "",
	}
	for in, want := range cases {
		if got := parseMountpoint(in); got != want {
			t.Errorf("parseMountpoint(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMapUdisksErr(t *testing.T) {
	cases := []struct {
		out  string
		want error
	}{
		{"Error mounting: GDBus.Error:...NotAuthorized: Not authorized to perform operation", ErrForbidden},
		{"Error unmounting: target is busy", ErrBusy},
		{"Object /org/... is in use", ErrBusy},
		{"some other failure", ErrCommand},
	}
	for _, c := range cases {
		if got := mapUdisksErr([]byte(c.out)); !errors.Is(got, c.want) {
			t.Errorf("mapUdisksErr(%q) = %v, want %v", c.out, got, c.want)
		}
	}
}

func TestSafeID(t *testing.T) {
	ok := []string{"sdc1", "nvme0n1p2", "mmcblk0p1", "sda", "loop0"}
	for _, n := range ok {
		if id, valid := safeID(n); !valid || id != n {
			t.Errorf("safeID(%q) should be valid", n)
		}
	}
	bad := []string{"", "..", ".", "../etc", "a/b", "a b", "a;b", "名字"}
	for _, n := range bad {
		if _, valid := safeID(n); valid {
			t.Errorf("safeID(%q) should be invalid", n)
		}
	}
}
