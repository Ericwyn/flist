//go:build linux

package device

import (
	"errors"
	"testing"
)

func TestParseLsblk(t *testing.T) {
	// 覆盖：整盘含分区、已挂载分区、只读、无 fstype 分区、含卷标、无分区表的 U 盘、
	// 以及 size 为字符串 / rm/ro 为数字的旧版本表示。
	sample := []byte(`{
      "blockdevices": [
        {
          "name":"sda","path":"/dev/sda","type":"disk","size":500107862016,
          "fstype":null,"label":null,"mountpoint":null,"rm":false,"ro":false,
          "children":[
            {"name":"sda1","path":"/dev/sda1","type":"part","size":524288000,
             "fstype":"vfat","label":"BOOT","mountpoint":"/boot","rm":false,"ro":false},
            {"name":"sda2","path":"/dev/sda2","type":"part","size":499582500864,
             "fstype":"ext4","label":"root","mountpoint":"/","rm":false,"ro":false}
          ]
        },
        {
          "name":"sdc","path":"/dev/sdc","type":"disk","size":"61530439680",
          "fstype":null,"label":null,"mountpoint":null,"rm":"1","ro":"0",
          "children":[
            {"name":"sdc1","path":"/dev/sdc1","type":"part","size":"61530000000",
             "fstype":"exfat","label":"KINGSTON","mountpoint":null,"rm":1,"ro":0}
          ]
        },
        {
          "name":"sdd","path":"/dev/sdd","type":"disk","size":16000000000,
          "fstype":"ntfs","label":"USBSTICK","mountpoint":"/run/media/u/USBSTICK","rm":true,"ro":true
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

	// sda1 / sda2 分区应被收录。
	sda1, ok := byDev["/dev/sda1"]
	if !ok {
		t.Fatal("expected /dev/sda1 to be parsed")
	}
	if sda1.ID != "sda1" || sda1.FSType != "vfat" || sda1.Label != "BOOT" {
		t.Errorf("sda1 fields wrong: %+v", sda1)
	}
	if !sda1.Mounted || sda1.Mountpoint != "/boot" {
		t.Errorf("sda1 should be mounted at /boot: %+v", sda1)
	}

	// sdc1：size/rm/ro 为字符串或数字，应正确解析；未挂载；卷标 KINGSTON；可移动。
	sdc1, ok := byDev["/dev/sdc1"]
	if !ok {
		t.Fatal("expected /dev/sdc1 to be parsed")
	}
	if sdc1.Size != 61530000000 {
		t.Errorf("sdc1 size mismatch: %d", sdc1.Size)
	}
	if sdc1.Mounted {
		t.Errorf("sdc1 should not be mounted: %+v", sdc1)
	}
	if !sdc1.Removable {
		t.Errorf("sdc1 should be removable (rm=1): %+v", sdc1)
	}

	// sdd：无分区表整盘、有 fstype、只读、已挂载 → 应收录。
	sdd, ok := byDev["/dev/sdd"]
	if !ok {
		t.Fatal("expected whole-disk /dev/sdd (fstype ntfs) to be parsed")
	}
	if !sdd.Readonly {
		t.Errorf("sdd should be readonly: %+v", sdd)
	}
	if !sdd.Mounted || sdd.Mountpoint != "/run/media/u/USBSTICK" {
		t.Errorf("sdd mountpoint wrong: %+v", sdd)
	}

	// sda / sdc（有子分区的整盘、无 fstype）不应作为可挂载条目收录。
	if _, ok := byDev["/dev/sda"]; ok {
		t.Errorf("whole disk /dev/sda with children should not be listed")
	}
	if _, ok := byDev["/dev/sdc"]; ok {
		t.Errorf("whole disk /dev/sdc with children should not be listed")
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
