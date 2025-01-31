package sniffer

import (
	"bytes"
	"reflect"
	"testing"
)

func TestReadOnlyConn(t *testing.T) {
	type args struct {
		b []byte
	}
	tests := []struct {
		name string
		args args
		want []byte
	}{
		{
			name: "1",
			args: args{
				b: []byte{
					0x6e, 0x62, 0xe5, 0x1a, 0x43, 0x26, 0xb0, 0xac, 0xf9,
					0xc2, 0x20, 0x8b, 0x90, 0xd0, 0x36, 0x72, 0x51, 0x7b,
					0x5c, 0x85, 0x85, 0xf2, 0x6a, 0x18, 0xb1, 0x27, 0xa6,
					0x5d, 0x9c, 0xe9, 0x6a, 0x12, 0x4e, 0x17, 0x3d, 0xe5,
					0xe9, 0xe3, 0xa1, 0x5, 0x7c, 0x9a, 0x9, 0x6, 0x4c,
					0x51, 0x1f, 0xd9, 0xc5, 0x6f, 0xf9,
				},
			},
			want: []byte{
				0x6e, 0x62, 0xe5, 0x1a, 0x43, 0x26, 0xb0, 0xac, 0xf9,
				0xc2, 0x20, 0x8b, 0x90, 0xd0, 0x36, 0x72, 0x51, 0x7b,
				0x5c, 0x85, 0x85, 0xf2, 0x6a, 0x18, 0xb1, 0x27, 0xa6,
				0x5d, 0x9c, 0xe9, 0x6a, 0x12, 0x4e, 0x17, 0x3d, 0xe5,
				0xe9, 0xe3, 0xa1, 0x5, 0x7c, 0x9a, 0x9, 0x6, 0x4c,
				0x51, 0x1f, 0xd9, 0xc5, 0x6f, 0xf9,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := make([]byte, 1024)
			c1 := newFakeTestConn(bytes.NewReader(tt.args.b))
			readOnlyConn := StreamReadOnlyConn(c1)
			n, _ := readOnlyConn.Read(buf)
			if got := buf[:n]; !reflect.DeepEqual(got, tt.want) {
				t.Errorf("readOnlyConn Read() = %v, want %v", got, tt.want)
			}
			originConn := readOnlyConn.UnreadConn()
			clear(buf)
			n, _ = originConn.Read(buf)
			if got := buf[:n]; !reflect.DeepEqual(got, tt.want) {
				t.Errorf("unreadConn Read() = %v, want %v", got, tt.want)
			}
		})
	}
}
