package ceph

import "testing"

func TestFilterCandidateDisks(t *testing.T) {
	raw := `{
  "blockdevices": [
    {"name":"sda","size":"100G","type":"disk","mountpoint":null,"model":null,"serial":null,"children":[
      {"name":"sda1","size":"1G","type":"part","mountpoint":"/boot"},
      {"name":"sda2","size":"99G","type":"part","mountpoint":"/"}
    ]},
    {"name":"sdb","size":"500G","type":"disk","mountpoint":null,"rota":true,"model":null,"serial":null},
    {"name":"sdc","size":"50G","type":"disk","mountpoint":null,"rota":false,"model":"SSD","serial":"ABC"},
    {"name":"loop0","size":"1G","type":"loop","mountpoint":null},
    {"name":"nbd0","size":"0B","type":"disk","mountpoint":null},
    {"name":"nbd15","size":"0B","type":"disk","mountpoint":null},
    {"name":"zram0","size":"8G","type":"disk","mountpoint":null},
    {"name":"sr0","size":"3.8G","type":"rom","mountpoint":null}
  ]
}`
	got := FilterCandidateDevices(raw)
	if len(got) != 2 {
		t.Fatalf("expected 2 disks, got %#v", got)
	}
	if got[0].Name != "sdb" || got[1].Name != "sdc" {
		t.Fatalf("unexpected names: %#v", got)
	}
}
