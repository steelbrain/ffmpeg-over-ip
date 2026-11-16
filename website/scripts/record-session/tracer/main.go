// tracer: transparent TCP proxy that logs ffmpeg-over-ip protocol messages as JSONL.
package main

import (
	"encoding/binary"
	"encoding/json"
	"flag"
	"io"
	"log"
	"net"
	"os"
	"sync"
	"time"
)

var names = map[uint8]string{
	0x01: "COMMAND", 0x02: "CANCEL", 0x03: "EXIT_CODE", 0x04: "ERROR", 0x05: "PING", 0x06: "PONG",
	0x07: "HELLO", 0x08: "CHALLENGE", 0x09: "ACCEPTED",
	0x10: "STDIN", 0x11: "STDIN_CLOSE", 0x12: "STDOUT", 0x13: "STDERR", 0x14: "STDIN_ACK",
	0x20: "OPEN", 0x21: "READ", 0x22: "WRITE", 0x23: "SEEK", 0x24: "CLOSE", 0x25: "FSTAT",
	0x26: "FTRUNCATE", 0x27: "UNLINK", 0x28: "RENAME", 0x29: "MKDIR", 0x2A: "STAT", 0x2B: "LISTDIR",
	0x40: "OPEN_OK", 0x41: "READ_OK", 0x42: "WRITE_OK", 0x43: "SEEK_OK", 0x44: "CLOSE_OK", 0x45: "FSTAT_OK",
	0x46: "FTRUNCATE_OK", 0x47: "UNLINK_OK", 0x48: "RENAME_OK", 0x49: "MKDIR_OK", 0x4A: "STAT_OK",
	0x4B: "LISTDIR_OK", 0x4F: "IO_ERROR",
}

type rec map[string]any

var (
	mu  sync.Mutex
	enc *json.Encoder
)

func emit(r rec) { mu.Lock(); enc.Encode(r); mu.Unlock() }

func u16(b []byte, o int) uint16 { return binary.BigEndian.Uint16(b[o:]) }
func u32(b []byte, o int) uint32 { return binary.BigEndian.Uint32(b[o:]) }
func i64(b []byte, o int) int64  { return int64(binary.BigEndian.Uint64(b[o:])) }

func decode(t uint8, p []byte, r rec) {
	defer func() { recover() }()
	switch t {
	case 0x07:
		r["version"] = p[0]
	case 0x01:
		r["version"] = p[0]
		off := 1 + 16 + 32
		r["program"] = p[off]
		off++
		argc := int(u16(p, off))
		off += 2
		args := []string{}
		for i := 0; i < argc; i++ {
			l := int(u16(p, off))
			off += 2
			args = append(args, string(p[off:off+l]))
			off += l
		}
		r["args"] = args
	case 0x03:
		r["code"] = u32(p, 0)
	case 0x04, 0x12, 0x13:
		r["text"] = string(p)
	case 0x20:
		r["req"], r["fid"], r["flags"], r["mode"], r["path"] = u16(p, 0), u16(p, 2), u32(p, 4), u16(p, 8), string(p[10:])
	case 0x21:
		r["req"], r["fid"], r["n"] = u16(p, 0), u16(p, 2), u32(p, 4)
	case 0x22:
		r["req"], r["fid"], r["n"] = u16(p, 0), u16(p, 2), len(p)-4
	case 0x23:
		r["req"], r["fid"], r["offset"], r["whence"] = u16(p, 0), u16(p, 2), i64(p, 4), p[12]
	case 0x24, 0x25:
		r["req"], r["fid"] = u16(p, 0), u16(p, 2)
	case 0x26:
		r["req"], r["fid"], r["length"] = u16(p, 0), u16(p, 2), i64(p, 4)
	case 0x27, 0x2A, 0x2B:
		r["req"], r["path"] = u16(p, 0), string(p[2:])
	case 0x28:
		l := int(u16(p, 2))
		r["req"], r["old"], r["new"] = u16(p, 0), string(p[4:4+l]), string(p[4+l:])
	case 0x29:
		r["req"], r["mode"], r["path"] = u16(p, 0), u16(p, 2), string(p[4:])
	case 0x40, 0x45:
		r["req"], r["size"], r["mode"] = u16(p, 0), i64(p, 2), u32(p, 10)
	case 0x41:
		r["req"], r["n"] = u16(p, 0), len(p)-2
	case 0x42:
		r["req"], r["n"] = u16(p, 0), u32(p, 2)
	case 0x43:
		r["req"], r["offset"] = u16(p, 0), i64(p, 2)
	case 0x44, 0x46, 0x47, 0x48, 0x49:
		r["req"] = u16(p, 0)
	case 0x4A:
		r["req"], r["size"], r["mode"] = u16(p, 0), i64(p, 2), u32(p, 10)
	case 0x4B:
		r["req"], r["entries"] = u16(p, 0), u32(p, 2)
	case 0x4F:
		r["req"], r["errno"] = u16(p, 0), int32(u32(p, 2))
	}
}

func pump(dst, src net.Conn, dir string, start time.Time, conn int) {
	defer dst.(*net.TCPConn).CloseWrite()
	var hdr [5]byte
	for {
		if _, err := io.ReadFull(src, hdr[:]); err != nil {
			return
		}
		n := binary.BigEndian.Uint32(hdr[1:])
		p := make([]byte, n)
		if _, err := io.ReadFull(src, p); err != nil {
			return
		}
		t := time.Since(start)
		name := names[hdr[0]]
		if name == "" {
			name = "UNKNOWN"
		}
		r := rec{"conn": conn, "t": float64(t.Microseconds()) / 1000, "dir": dir, "type": name, "len": n}
		decode(hdr[0], p, r)
		emit(r)
		dst.Write(hdr[:])
		dst.Write(p)
	}
}

func main() {
	listen := flag.String("listen", "127.0.0.1:5050", "")
	up := flag.String("upstream", "", "")
	out := flag.String("out", "trace.jsonl", "")
	flag.Parse()
	f, err := os.Create(*out)
	if err != nil {
		log.Fatal(err)
	}
	enc = json.NewEncoder(f)
	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		log.Fatal(err)
	}
	conn := 0
	for {
		c, err := ln.Accept()
		if err != nil {
			log.Fatal(err)
		}
		conn++
		id := conn
		go func() {
			start := time.Now()
			u, err := net.Dial("tcp", *up)
			if err != nil {
				log.Print(err)
				c.Close()
				return
			}
			emit(rec{"conn": id, "t": 0, "dir": "meta", "type": "CONNECT", "local": c.LocalAddr().String(), "upstream_local": u.LocalAddr().String(), "upstream_remote": u.RemoteAddr().String()})
			var wg sync.WaitGroup
			wg.Add(2)
			go func() { pump(u, c, "c2s", start, id); wg.Done() }()
			go func() { pump(c, u, "s2c", start, id); wg.Done() }()
			wg.Wait()
			emit(rec{"conn": id, "t": float64(time.Since(start).Microseconds()) / 1000, "dir": "meta", "type": "DONE"})
			c.Close()
			u.Close()
		}()
	}
}
