package filehandler

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/steelbrain/ffmpeg-over-ip/internal/protocol"
)

// Handler executes file I/O operations against the local filesystem.
type Handler struct {
	mu    sync.Mutex
	files map[uint16]*os.File
}

func NewHandler() *Handler {
	return &Handler{
		files: make(map[uint16]*os.File),
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
		return h.handleRead(payload)
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
	default:
		return 0, nil, fmt.Errorf("unknown message type: 0x%02x", msgType)
	}
}

// CloseAll closes all open file handles. Used on session teardown.
func (h *Handler) CloseAll() {
	h.mu.Lock()
	defer h.mu.Unlock()

	for id, f := range h.files {
		f.Close()
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

	h.files[req.FileID] = f

	resp := &protocol.OpenOkResponse{RequestID: req.RequestID, FileSize: fileSize}
	return protocol.MsgOpenOk, resp.Encode(), nil
}

func (h *Handler) handleRead(payload []byte) (uint8, []byte, error) {
	req, err := protocol.DecodeReadRequest(payload)
	if err != nil {
		return 0, nil, err
	}

	f, ok := h.files[req.FileID]
	if !ok {
		return protocol.MsgIoError, ioErr(req.RequestID, protocol.FioEINVAL), nil
	}

	buf := make([]byte, req.NBytes)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return protocol.MsgIoError, ioErr(req.RequestID, mapErrno(err)), nil
	}

	resp := &protocol.ReadOkResponse{RequestID: req.RequestID, Data: buf[:n]}
	return protocol.MsgReadOk, resp.Encode(), nil
}

func (h *Handler) handleWrite(payload []byte) (uint8, []byte, error) {
	req, err := protocol.DecodeWriteRequest(payload)
	if err != nil {
		return 0, nil, err
	}

	f, ok := h.files[req.FileID]
	if !ok {
		return protocol.MsgIoError, ioErr(req.RequestID, protocol.FioEINVAL), nil
	}

	n, err := f.Write(req.Data)
	if err != nil {
		return protocol.MsgIoError, ioErr(req.RequestID, mapErrno(err)), nil
	}

	resp := &protocol.WriteOkResponse{RequestID: req.RequestID, BytesWritten: uint32(n)}
	return protocol.MsgWriteOk, resp.Encode(), nil
}

func (h *Handler) handleSeek(payload []byte) (uint8, []byte, error) {
	req, err := protocol.DecodeSeekRequest(payload)
	if err != nil {
		return 0, nil, err
	}

	f, ok := h.files[req.FileID]
	if !ok {
		return protocol.MsgIoError, ioErr(req.RequestID, protocol.FioEINVAL), nil
	}

	whence, err := wireToWhence(req.Whence)
	if err != nil {
		return protocol.MsgIoError, ioErr(req.RequestID, protocol.FioEINVAL), nil
	}

	offset, err := f.Seek(req.Offset, whence)
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

	f, ok := h.files[req.FileID]
	if !ok {
		return protocol.MsgIoError, ioErr(req.RequestID, protocol.FioEINVAL), nil
	}

	err = f.Close()
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

	f, ok := h.files[req.FileID]
	if !ok {
		return protocol.MsgIoError, ioErr(req.RequestID, protocol.FioEINVAL), nil
	}

	info, err := f.Stat()
	if err != nil {
		return protocol.MsgIoError, ioErr(req.RequestID, mapErrno(err)), nil
	}

	resp := &protocol.FstatOkResponse{
		RequestID: req.RequestID,
		FileSize:  info.Size(),
		Mode:      uint32(info.Mode()),
	}
	return protocol.MsgFstatOk, resp.Encode(), nil
}

func (h *Handler) handleFtruncate(payload []byte) (uint8, []byte, error) {
	req, err := protocol.DecodeFtruncateRequest(payload)
	if err != nil {
		return 0, nil, err
	}

	f, ok := h.files[req.FileID]
	if !ok {
		return protocol.MsgIoError, ioErr(req.RequestID, protocol.FioEINVAL), nil
	}

	if err := f.Truncate(req.Length); err != nil {
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
func mapErrno(err error) int32 {
	var errno syscall.Errno
	if errors.As(err, &errno) {
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
		}
	}
	return protocol.FioEIO
}

func ioErr(requestID uint16, errno int32) []byte {
	return (&protocol.IoErrorResponse{RequestID: requestID, Errno: errno}).Encode()
}
