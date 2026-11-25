package identity

import (
	"reflect"
	"testing"
)

func TestNormalizeMAC(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"AA:BB:CC:DD:EE:FF", "aa:bb:cc:dd:ee:ff", false},
		{"aa-bb-cc-dd-ee-ff", "aa:bb:cc:dd:ee:ff", false},
		{"aabb.ccdd.eeff", "aa:bb:cc:dd:ee:ff", false},
		{"0xAABBCCDDEEFF", "aa:bb:cc:dd:ee:ff", false},
		{"invalid", "", true},
	}
	for _, tc := range cases {
		got, err := NormalizeMAC(tc.in)
		if (err != nil) != tc.wantErr {
			t.Fatalf("NormalizeMAC(%q) err=%v, wantErr=%v", tc.in, err, tc.wantErr)
		}
		if got != tc.want {
			t.Fatalf("NormalizeMAC(%q)=%q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeSerialAndUUID(t *testing.T) {
	if got := NormalizeSerial(" to be filled by o.e.m. "); got != "" {
		t.Fatalf("expected junk serial filtered, got %q", got)
	}
	if got := NormalizeSerial("hl51jz3"); got != "HL51JZ3" {
		t.Fatalf("got %q, want HL51JZ3", got)
	}
	if got := NormalizeUUID("00000000-0000-0000-0000-000000000000"); got != "" {
		t.Fatalf("expected zero UUID filtered, got %q", got)
	}
	if got := NormalizeUUID("4C4C4544005442108037B8C04F4A3333"); got != "4c4c4544-0054-4210-8037-b8c04f4a3333" {
		t.Fatalf("unexpected compact UUID norm: %q", got)
	}
}

func TestPortCandidates(t *testing.T) {
	got := PortCandidates("0/25", "Port 25", "")
	want := []string{"Port 25"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if u := UnifiPortCandidate("eth0"); u != "Port 1" {
		t.Fatalf("UnifiPortCandidate(eth0)=%q, want Port 1", u)
	}
}

func TestLookupVendor(t *testing.T) {
	if v := LookupVendor("6c:3c:8c:75:3e:7e"); v != "Dell" {
		t.Fatalf("expected Dell, got %q", v)
	}
	if v := LookupVendor("24:5a:4c:76:79:d1"); v != "Ubiquiti" {
		t.Fatalf("expected Ubiquiti, got %q", v)
	}
}
