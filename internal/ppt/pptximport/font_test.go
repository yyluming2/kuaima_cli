package pptximport

import "testing"

func TestUUIDWireKey(t *testing.T) {
	in := []byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}
	want := []byte{0x33, 0x22, 0x11, 0x00, 0x55, 0x44, 0x77, 0x66, 0x88, 0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}
	got := uuidWireKey(in)
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("byte %d: got %02x, want %02x", i, got[i], want[i])
		}
	}
}

func TestDecodeObfuscatedFontUUIDVariants(t *testing.T) {
	key := []byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}
	plain := append([]byte("OTTO"), make([]byte, 28)...)
	wire := uuidWireKey(key)
	raw := append([]byte(nil), plain...)
	for i := 0; i < 32; i++ {
		raw[i] ^= wire[i%16]
	}
	decoded, ok := decodeObfuscatedFont(raw, key)
	if !ok || string(decoded[:4]) != "OTTO" {
		t.Fatalf("font was not decoded: ok=%v signature=%q", ok, decoded[:4])
	}
}

func TestUnwrapFontContainer(t *testing.T) {
	raw := append([]byte("BSGP"), make([]byte, 288)...)
	raw = append(raw, []byte{0, 1, 0, 0, 1, 2, 3}...)
	font, offset := unwrapFontContainer(raw)
	if offset != 292 || string(font[:4]) != "\x00\x01\x00\x00" {
		t.Fatalf("unexpected unwrap offset/signature: offset=%d signature=%x", offset, font[:4])
	}
}
