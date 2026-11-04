package filehandler

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/steelbrain/ffmpeg-over-ip/internal/protocol"
)

// maxListDirPayload caps a ListDirOk reply well below the 100 MiB message
// limit both ends enforce; a bigger directory is answered with ERANGE.
const maxListDirPayload = 64 * 1024 * 1024

// listDirBatch is how many directory entries ListDir reads at a time.
const listDirBatch = 1024

// maxReadBytes caps one READ reply. fio never asks for more (FIO_MAX_IO_BYTES);
// a larger request gets a short read, as read(2) allows, instead of a
// multi-gigabyte allocation and a reply over the message limit.
const maxReadBytes = 16 * 1024 * 1024

// Handler executes file I/O operations against the local filesystem.
type Handler struct {
	mu      sync.Mutex
	files   map[uint16]*openFile
	readBuf []byte
}

type openFile struct {
	f *os.File
	// regular files are read until the request is filled or EOF; pipes and
	// devices get a single read so a reply never waits for more data.
	regular bool
}

type messageWriter interface {
	WriteMessage(msgType uint8, payload []byte) error
}

func NewHandler() *Handler {
	return &Handler{
		files: make(map[uint16]*openFile),
	}
}

// HandleMessage dispatches a decoded file I/O request and returns the response
// type and encoded payload. The error return is only for unknown/undecodable
// messages — filesystem errors are returned as (MsgIoError, encoded IoErrorResponse, nil).
func (h *Handler) HandleMessage(msgType uint8, payload []byte) (uint8, []byte, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	switch msgType {
	case protocol.MsgOpen:
		return h.handleOpen(payload)
	case protocol.MsgRead:
		return h.handleRead(payload, nil)
	case protocol.MsgWrite:
		return h.handleWrite(payload)
	case protocol.MsgSeek:
		return h.handleSeek(payload)
	case protocol.MsgClose:
		return h.handleClose(payload)
	case protocol.MsgFstat:
		return h.handleFstat(payload)
	case protocol.MsgFtruncate:
		return h.handleFtruncate(payload)
	case protocol.MsgUnlink:
		return h.handleUnlink(payload)
	case protocol.MsgRename:
		return h.handleRename(payload)
	case protocol.MsgMkdir:
		return h.handleMkdir(payload)
	case protocol.MsgStat:
		return h.handleStat(payload)
	case protocol.MsgListDir:
		return h.handleListDir(payload)
	default:
		return 0, nil, fmt.Errorf("unknown message type: 0x%02x", msgType)
	}
}

// HandleReadTo handles a read request and writes the response before returning.
// It reuses an internal response buffer, so callers must not retain the payload.
func (h *Handler) HandleReadTo(payload []byte, w messageWriter) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	respType, respPayload, err := h.handleRead(payload, &h.readBuf)
	if err != nil {
		return err
	}
	return w.WriteMessage(respType, respPayload)
}

// CloseAll closes all open file handles. Used on session teardown.
func (h *Handler) CloseAll() {
	h.mu.Lock()
	defer h.mu.Unlock()

	for id, of := range h.files {
		of.f.Close()
		delete(h.files, id)
	}
}

func (h *Handler) handleOpen(payload []byte) (uint8, []byte, error) {
	req, err := protocol.DecodeOpenRequest(payload)
	if err != nil {
		return 0, nil, err
	}

	if _, exists := h.files[req.FileID]; exists {
		return protocol.MsgIoError, ioErr(req.RequestID, protocol.FioEINVAL), nil
	}

	osFlags := wireToOSFlags(req.Flags)
	mode := os.FileMode(req.Mode)

	if req.Flags&protocol.FioOCREAT != 0 {
		dir := filepath.Dir(req.Path)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return protocol.MsgIoError, ioErr(req.RequestID, mapErrno(err)), nil
		}
	}

	f, err := os.OpenFile(req.Path, osFlags, mode)
	if err != nil {
		return protocol.MsgIoError, ioErr(req.RequestID, mapErrno(err)), nil
	}

	var fileSize int64
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return protocol.MsgIoError, ioErr(req.RequestID, mapErrno(err)), nil
	}
	fileSize = info.Size()

	h.files[req.FileID] = &openFile{f: f, regular: info.Mode().IsRegular()}

	resp := &protocol.OpenOkResponse{
		RequestID: req.RequestID,
		FileSize:  fileSize,
		Mode:      protocol.ModeToWire(info.Mode()),
	}
	return protocol.MsgOpenOk, resp.Encode(), nil
}

func (h *Handler) handleRead(payload []byte, reusable *[]byte) (uint8, []byte, error) {
	req, err := protocol.DecodeReadRequest(payload)
	if err != nil {
		return 0, nil, err
	}

	of, ok := h.files[req.FileID]
	if !ok {
		return protocol.MsgIoError, ioErr(req.RequestID, protocol.FioEINVAL), nil
	}

	nbytes := int(req.NBytes)
	if nbytes > maxReadBytes {
		nbytes = maxReadBytes
	}
	responseLen := 2 + nbytes
	var resp []byte
	if reusable != nil {
		if cap(*reusable) < responseLen {
			*reusable = make([]byte, responseLen)
		}
		resp = (*reusable)[:responseLen]
	} else {
		resp = make([]byte, responseLen)
	}
	binary.BigEndian.PutUint16(resp[0:], req.RequestID)
	n, err := readRequest(of.f, of.regular, resp[2:])
	if n == 0 && err != nil && err != io.EOF {
		return protocol.MsgIoError, ioErr(req.RequestID, mapErrno(err)), nil
	}
	// Bytes already read have moved the file offset, so they are returned
	// even if a later read in the same request failed; the error shows up
	// on the next request, as with read(2).
	return protocol.MsgReadOk, resp[:2+n], nil
}

// readRequest reads into buf. A single read(2) can come back short in the
// middle of a file on network or FUSE storage, and fio only treats an empty
// reply as end of file, so regular files are read until buf is full or EOF.
func readRequest(r io.Reader, regular bool, buf []byte) (int, error) {
	if !regular {
		return r.Read(buf)
	}
	n := 0
	for n < len(buf) {
		m, err := r.Read(buf[n:])
		n += m
		if err != nil {
			return n, err
		}
		if m == 0 {
			break
		}
	}
	return n, nil
}

func (h *Handler) handleWrite(payload []byte) (uint8, []byte, error) {
	req, err := protocol.DecodeWriteRequest(payload)
	if err != nil {
		return 0, nil, err
	}

	of, ok := h.files[req.FileID]
	if !ok {
		return protocol.MsgIoError, ioErr(req.RequestID, protocol.FioEINVAL), nil
	}

	n, err := of.f.Write(req.Data)
	return writeReply(req.RequestID, n, err)
}

// writeReply answers a WRITE that wrote n bytes and then failed with err (or
// not). Bytes already written have moved the file offset, so like write(2)
// a partial write is reported as a short count, and the error shows up on
// the next request. Reporting it as an error instead would leave fio
// thinking the offset hadn't moved, and its next write would land n bytes
// too far.
func writeReply(requestID uint16, n int, err error) (uint8, []byte, error) {
	if n == 0 && err != nil {
		return protocol.MsgIoError, ioErr(requestID, mapErrno(err)), nil
	}
	resp := &protocol.WriteOkResponse{RequestID: requestID, BytesWritten: uint32(n)}
	return protocol.MsgWriteOk, resp.Encode(), nil
}

func (h *Handler) handleSeek(payload []byte) (uint8, []byte, error) {
	req, err := protocol.DecodeSeekRequest(payload)
	if err != nil {
		return 0, nil, err
	}

	of, ok := h.files[req.FileID]
	if !ok {
		return protocol.MsgIoError, ioErr(req.RequestID, protocol.FioEINVAL), nil
	}

	whence, err := wireToWhence(req.Whence)
	if err != nil {
		return protocol.MsgIoError, ioErr(req.RequestID, protocol.FioEINVAL), nil
	}

	offset, err := of.f.Seek(req.Offset, whence)
	if err != nil {
		return protocol.MsgIoError, ioErr(req.RequestID, mapErrno(err)), nil
	}

	resp := &protocol.SeekOkResponse{RequestID: req.RequestID, Offset: offset}
	return protocol.MsgSeekOk, resp.Encode(), nil
}

func (h *Handler) handleClose(payload []byte) (uint8, []byte, error) {
	req, err := protocol.DecodeCloseRequest(payload)
	if err != nil {
		return 0, nil, err
	}

	of, ok := h.files[req.FileID]
	if !ok {
		return protocol.MsgIoError, ioErr(req.RequestID, protocol.FioEINVAL), nil
	}

	err = of.f.Close()
	delete(h.files, req.FileID)
	if err != nil {
		return protocol.MsgIoError, ioErr(req.RequestID, mapErrno(err)), nil
	}

	resp := &protocol.RequestIDResponse{RequestID: req.RequestID}
	return protocol.MsgCloseOk, resp.Encode(), nil
}

func (h *Handler) handleFstat(payload []byte) (uint8, []byte, error) {
	req, err := protocol.DecodeFstatRequest(payload)
	if err != nil {
		return 0, nil, err
	}

	of, ok := h.files[req.FileID]
	if !ok {
		return protocol.MsgIoError, ioErr(req.RequestID, protocol.FioEINVAL), nil
	}

	info, err := of.f.Stat()
	if err != nil {
		return protocol.MsgIoError, ioErr(req.RequestID, mapErrno(err)), nil
	}

	resp := &protocol.FstatOkResponse{
		RequestID: req.RequestID,
		FileSize:  info.Size(),
		Mode:      protocol.ModeToWire(info.Mode()),
	}
	return protocol.MsgFstatOk, resp.Encode(), nil
}

func (h *Handler) handleFtruncate(payload []byte) (uint8, []byte, error) {
	req, err := protocol.DecodeFtruncateRequest(payload)
	if err != nil {
		return 0, nil, err
	}

	of, ok := h.files[req.FileID]
	if !ok {
		return protocol.MsgIoError, ioErr(req.RequestID, protocol.FioEINVAL), nil
	}

	if err := of.f.Truncate(req.Length); err != nil {
		return protocol.MsgIoError, ioErr(req.RequestID, mapErrno(err)), nil
	}

	resp := &protocol.RequestIDResponse{RequestID: req.RequestID}
	return protocol.MsgFtruncateOk, resp.Encode(), nil
}

func (h *Handler) handleUnlink(payload []byte) (uint8, []byte, error) {
	req, err := protocol.DecodeUnlinkRequest(payload)
	if err != nil {
		return 0, nil, err
	}

	if err := os.Remove(req.Path); err != nil {
		return protocol.MsgIoError, ioErr(req.RequestID, mapErrno(err)), nil
	}

	resp := &protocol.RequestIDResponse{RequestID: req.RequestID}
	return protocol.MsgUnlinkOk, resp.Encode(), nil
}

func (h *Handler) handleRename(payload []byte) (uint8, []byte, error) {
	req, err := protocol.DecodeRenameRequest(payload)
	if err != nil {
		return 0, nil, err
	}

	if err := os.Rename(req.OldPath, req.NewPath); err != nil {
		return protocol.MsgIoError, ioErr(req.RequestID, mapErrno(err)), nil
	}

	resp := &protocol.RequestIDResponse{RequestID: req.RequestID}
	return protocol.MsgRenameOk, resp.Encode(), nil
}

func (h *Handler) handleMkdir(payload []byte) (uint8, []byte, error) {
	req, err := protocol.DecodeMkdirRequest(payload)
	if err != nil {
		return 0, nil, err
	}

	if err := os.Mkdir(req.Path, os.FileMode(req.Mode)); err != nil {
		return protocol.MsgIoError, ioErr(req.RequestID, mapErrno(err)), nil
	}

	resp := &protocol.RequestIDResponse{RequestID: req.RequestID}
	return protocol.MsgMkdirOk, resp.Encode(), nil
}

func (h *Handler) handleStat(payload []byte) (uint8, []byte, error) {
	req, err := protocol.DecodePathRequest(payload)
	if err != nil {
		return 0, nil, err
	}

	// stat(2) and access(2) both follow symlinks.
	info, err := os.Stat(req.Path)
	if err != nil {
		return protocol.MsgIoError, ioErr(req.RequestID, mapErrno(err)), nil
	}

	resp := &protocol.StatOkResponse{
		RequestID: req.RequestID,
		Size:      info.Size(),
		Mode:      protocol.ModeToWire(info.Mode()),
		Mtime:     info.ModTime().Unix(),
		Access:    accessBits(req.Path, info),
	}
	return protocol.MsgStatOk, resp.Encode(), nil
}

func (h *Handler) handleListDir(payload []byte) (uint8, []byte, error) {
	req, err := protocol.DecodePathRequest(payload)
	if err != nil {
		return 0, nil, err
	}

	// Opening a FIFO blocks until a writer appears, and the handler serves
	// one request at a time: only open what is a directory.
	if info, err := os.Stat(req.Path); err != nil {
		return protocol.MsgIoError, ioErr(req.RequestID, mapErrno(err)), nil
	} else if !info.IsDir() {
		return protocol.MsgIoError, ioErr(req.RequestID, protocol.FioENOTDIR), nil
	}

	dir, err := os.Open(req.Path)
	if err != nil {
		return protocol.MsgIoError, ioErr(req.RequestID, mapErrno(err)), nil
	}
	defer dir.Close()

	resp := &protocol.ListDirOkResponse{RequestID: req.RequestID}
	size := 6
	for {
		// In batches, so a huge directory hits the size cap before it is
		// all in memory.
		dirEntries, err := dir.ReadDir(listDirBatch)
		for _, de := range dirEntries {
			name := de.Name()
			if len(name) > 0xFFFF {
				continue
			}
			size += protocol.ListDirEntrySize(name)
			if size > maxListDirPayload {
				return protocol.MsgIoError, ioErr(req.RequestID, protocol.FioERANGE), nil
			}
			entry := protocol.DirEntry{Name: name, Mode: protocol.ModeToWire(de.Type())}
			// Info is lstat(2); an entry removed since ReadDir keeps its type.
			if info, err := de.Info(); err == nil {
				entry.Mode = protocol.ModeToWire(info.Mode())
				entry.Size = info.Size()
				entry.Mtime = info.ModTime().Unix()
			}
			resp.Entries = append(resp.Entries, entry)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			// A partial listing would look complete to the caller.
			return protocol.MsgIoError, ioErr(req.RequestID, mapErrno(err)), nil
		}
	}
	return protocol.MsgListDirOk, resp.Encode(), nil
}

// wireToOSFlags translates wire protocol flags to os package constants.
func wireToOSFlags(wire uint32) int {
	flags := 0

	accmode := wire & 0x0003
	switch accmode {
	case protocol.FioORDONLY:
		flags |= os.O_RDONLY
	case protocol.FioOWRONLY:
		flags |= os.O_WRONLY
	case protocol.FioORDWR:
		flags |= os.O_RDWR
	}

	if wire&protocol.FioOCREAT != 0 {
		flags |= os.O_CREATE
	}
	if wire&protocol.FioOTRUNC != 0 {
		flags |= os.O_TRUNC
	}

	return flags
}

// wireToWhence translates wire whence values to io.Seek* constants.
func wireToWhence(w uint8) (int, error) {
	switch w {
	case protocol.FioSeekSet:
		return io.SeekStart, nil
	case protocol.FioSeekCur:
		return io.SeekCurrent, nil
	case protocol.FioSeekEnd:
		return io.SeekEnd, nil
	default:
		return 0, fmt.Errorf("invalid whence: %d", w)
	}
}

// mapErrno translates a Go error to a canonical wire errno value.
//
// We try the POSIX syscall.Errno match first to preserve fine-grained
// distinctions (EPERM vs EACCES, etc.) on Unix, then fall back to the
// cross-platform fs sentinels — those catch Windows error codes (e.g.,
// ERROR_FILE_NOT_FOUND, ERROR_ALREADY_EXISTS, ERROR_ACCESS_DENIED) that
// don't share numeric values with POSIX errnos.
func mapErrno(err error) int32 {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		// Platform codes first: on Windows syscall.ENOTDIR is
		// ERROR_PATH_NOT_FOUND, which means a missing parent directory.
		if wire, ok := mapPlatformErrno(errno); ok {
			return wire
		}
		switch errno {
		case syscall.EPERM:
			return protocol.FioEPERM
		case syscall.ENOENT:
			return protocol.FioENOENT
		case syscall.EIO:
			return protocol.FioEIO
		case syscall.EACCES:
			return protocol.FioEACCES
		case syscall.EEXIST:
			return protocol.FioEEXIST
		case syscall.ENOTDIR:
			return protocol.FioENOTDIR
		case syscall.EISDIR:
			return protocol.FioEISDIR
		case syscall.EINVAL:
			return protocol.FioEINVAL
		case syscall.ENOSPC:
			return protocol.FioENOSPC
		case syscall.EROFS:
			return protocol.FioEROFS
		case syscall.ERANGE:
			return protocol.FioERANGE
		case syscall.EBUSY:
			return protocol.FioEBUSY
		case syscall.EXDEV:
			return protocol.FioEXDEV
		case syscall.ENAMETOOLONG:
			return protocol.FioENAMETOOLONG
		case syscall.ENOTEMPTY:
			return protocol.FioENOTEMPTY
		case syscall.ELOOP:
			return protocol.FioELOOP
		case syscall.EBADF:
			return protocol.FioEBADF
		case syscall.EFBIG:
			return protocol.FioEFBIG
		case syscall.ESPIPE:
			return protocol.FioESPIPE
		case syscall.EPIPE:
			return protocol.FioEPIPE
		case syscall.EOVERFLOW:
			return protocol.FioEOVERFLOW
		case syscall.EDQUOT:
			return protocol.FioEDQUOT
		}
	}
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return protocol.FioENOENT
	case errors.Is(err, fs.ErrExist):
		return protocol.FioEEXIST
	case errors.Is(err, fs.ErrPermission):
		return protocol.FioEACCES
	case errors.Is(err, fs.ErrInvalid):
		return protocol.FioEINVAL
	}
	return protocol.FioEIO
}

func ioErr(requestID uint16, errno int32) []byte {
	return (&protocol.IoErrorResponse{RequestID: requestID, Errno: errno}).Encode()
}
