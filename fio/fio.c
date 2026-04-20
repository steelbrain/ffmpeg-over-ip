#include "fio.h"

#include <errno.h>
#include <pthread.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#ifdef _WIN32
#include <winsock2.h>
#include <ws2tcpip.h>
#include <io.h>
#include <direct.h>
#else
#include <sys/socket.h>
#include <netinet/in.h>
#include <arpa/inet.h>
#endif

/* ======================================================================
 * A. Constants
 * ====================================================================== */

/* Message type constants (must match Go protocol constants) */
#define FIO_MSG_OPEN        0x20
#define FIO_MSG_READ        0x21
#define FIO_MSG_WRITE       0x22
#define FIO_MSG_SEEK        0x23
#define FIO_MSG_CLOSE       0x24
#define FIO_MSG_FSTAT       0x25
#define FIO_MSG_FTRUNCATE   0x26
#define FIO_MSG_UNLINK      0x27
#define FIO_MSG_RENAME      0x28
#define FIO_MSG_MKDIR       0x29

#define FIO_MSG_OPEN_OK        0x40
#define FIO_MSG_READ_OK        0x41
#define FIO_MSG_WRITE_OK       0x42
#define FIO_MSG_SEEK_OK        0x43
#define FIO_MSG_CLOSE_OK       0x44
#define FIO_MSG_FSTAT_OK       0x45
#define FIO_MSG_FTRUNCATE_OK   0x46
#define FIO_MSG_UNLINK_OK      0x47
#define FIO_MSG_RENAME_OK      0x48
#define FIO_MSG_MKDIR_OK       0x49
#define FIO_MSG_IO_ERROR       0x4F

/* Canonical open flags (platform-independent wire values) */
#define FIO_O_RDONLY  0x0000
#define FIO_O_WRONLY  0x0001
#define FIO_O_RDWR    0x0002
#define FIO_O_CREAT   0x0040
#define FIO_O_TRUNC   0x0200

/* Maximum path length for encoded requests */
#define FIO_PATH_MAX  4096

/* Canonical whence values */
#define FIO_SEEK_SET  0
#define FIO_SEEK_CUR  1
#define FIO_SEEK_END  2

/* Canonical errno values (matching Linux) */
#define FIO_EPERM     1
#define FIO_ENOENT    2
#define FIO_EIO       5
#define FIO_EACCES   13
#define FIO_EEXIST   17
#define FIO_ENOTDIR  20
#define FIO_EISDIR   21
#define FIO_EINVAL   22
#define FIO_ENOSPC   28
#define FIO_EROFS    30
#define FIO_ERANGE   34

/* Virtual FD table */
#define FIO_VFD_BASE     10000
#define FIO_MAX_FILES    256

/* Pending request slots */
#define FIO_MAX_PENDING  64

/* ======================================================================
 * Big-endian helpers (manual byte assembly, no htonl)
 * ====================================================================== */

static inline void put_u16(uint8_t *buf, uint16_t v) {
    buf[0] = (uint8_t)(v >> 8);
    buf[1] = (uint8_t)(v);
}

static inline void put_u32(uint8_t *buf, uint32_t v) {
    buf[0] = (uint8_t)(v >> 24);
    buf[1] = (uint8_t)(v >> 16);
    buf[2] = (uint8_t)(v >> 8);
    buf[3] = (uint8_t)(v);
}

static inline void put_u64(uint8_t *buf, uint64_t v) {
    buf[0] = (uint8_t)(v >> 56);
    buf[1] = (uint8_t)(v >> 48);
    buf[2] = (uint8_t)(v >> 40);
    buf[3] = (uint8_t)(v >> 32);
    buf[4] = (uint8_t)(v >> 24);
    buf[5] = (uint8_t)(v >> 16);
    buf[6] = (uint8_t)(v >> 8);
    buf[7] = (uint8_t)(v);
}

static inline uint16_t get_u16(const uint8_t *buf) {
    return ((uint16_t)buf[0] << 8) | (uint16_t)buf[1];
}

static inline uint32_t get_u32(const uint8_t *buf) {
    return ((uint32_t)buf[0] << 24) | ((uint32_t)buf[1] << 16) |
           ((uint32_t)buf[2] << 8)  | (uint32_t)buf[3];
}

static inline uint64_t get_u64(const uint8_t *buf) {
    return ((uint64_t)buf[0] << 56) | ((uint64_t)buf[1] << 48) |
           ((uint64_t)buf[2] << 40) | ((uint64_t)buf[3] << 32) |
           ((uint64_t)buf[4] << 24) | ((uint64_t)buf[5] << 16) |
           ((uint64_t)buf[6] << 8)  | (uint64_t)buf[7];
}

/* ======================================================================
 * B. Global State
 * ====================================================================== */

typedef struct {
    int      active;
    uint16_t file_id;
    int64_t  cached_size;
    int      dirty;        /* set on write, invalidates fstat cache */
} fio_vfd_t;

typedef struct {
    int      used;
    uint16_t req_id;
    uint8_t  resp_type;
    uint8_t *resp_payload;
    uint32_t resp_len;
} fio_pending_t;

static struct {
    int               initialized;  /* 0=uninit, 1=passthrough, 2=tunneled */
    int               sock_fd;
    uint16_t          next_file_id;
    uint16_t          next_req_id;
    pthread_mutex_t   send_mutex;
    pthread_mutex_t   dispatch_mutex;
    pthread_cond_t    dispatch_cond;
    fio_vfd_t         vfds[FIO_MAX_FILES];
    fio_pending_t     pending[FIO_MAX_PENDING];
    pthread_t         reader_thread;
} fio_state;

static pthread_once_t fio_once = PTHREAD_ONCE_INIT;

/* ======================================================================
 * C. Envelope encoding/decoding
 * ====================================================================== */

/* Encode envelope: type(1) + length(4) + payload */
__attribute__((unused))
static int encode_envelope(uint8_t type, const uint8_t *payload, uint32_t payload_len,
                           uint8_t *out, uint32_t out_cap) {
    uint32_t total = 5 + payload_len;
    if (out_cap < total) return -1;
    out[0] = type;
    put_u32(out + 1, payload_len);
    if (payload_len > 0 && payload)
        memcpy(out + 5, payload, payload_len);
    return (int)total;
}

/* Decode envelope from buffer. Returns total bytes consumed, -1 on error.
 * Sets *type_out, *payload_out (pointer into buf), *payload_len_out. */
__attribute__((unused))
static int decode_envelope(const uint8_t *buf, uint32_t buf_len,
                           uint8_t *type_out, const uint8_t **payload_out,
                           uint32_t *payload_len_out) {
    if (buf_len < 5) return -1;
    *type_out = buf[0];
    uint32_t plen = get_u32(buf + 1);
    if (buf_len < 5 + plen) return -1;
    *payload_out = buf + 5;
    *payload_len_out = plen;
    return (int)(5 + plen);
}

/* ======================================================================
 * D. Flag/Errno Translation
 * ====================================================================== */

static uint32_t flags_to_wire(int platform_flags) {
    uint32_t wire = 0;

    int accmode = platform_flags & O_ACCMODE;
    if (accmode == O_RDONLY) wire |= FIO_O_RDONLY;
    else if (accmode == O_WRONLY) wire |= FIO_O_WRONLY;
    else if (accmode == O_RDWR)   wire |= FIO_O_RDWR;

    if (platform_flags & O_CREAT)  wire |= FIO_O_CREAT;
    if (platform_flags & O_TRUNC)  wire |= FIO_O_TRUNC;

    return wire;
}

__attribute__((unused))
static int flags_from_wire(uint32_t wire) {
    int flags = 0;

    uint32_t accmode = wire & 0x0003;
    if (accmode == FIO_O_RDONLY)      flags |= O_RDONLY;
    else if (accmode == FIO_O_WRONLY) flags |= O_WRONLY;
    else if (accmode == FIO_O_RDWR)   flags |= O_RDWR;

    if (wire & FIO_O_CREAT)  flags |= O_CREAT;
    if (wire & FIO_O_TRUNC)  flags |= O_TRUNC;

    return flags;
}

__attribute__((unused))
static int32_t errno_to_wire(int err) {
    switch (err) {
    case EPERM:   return FIO_EPERM;
    case ENOENT:  return FIO_ENOENT;
    case EIO:     return FIO_EIO;
    case EACCES:  return FIO_EACCES;
    case EEXIST:  return FIO_EEXIST;
    case ENOTDIR: return FIO_ENOTDIR;
    case EISDIR:  return FIO_EISDIR;
    case EINVAL:  return FIO_EINVAL;
    case ENOSPC:  return FIO_ENOSPC;
#ifdef EROFS
    case EROFS:   return FIO_EROFS;
#endif
    case ERANGE:  return FIO_ERANGE;
    default:      return FIO_EIO;
    }
}

static int errno_from_wire(int32_t wire_err) {
    switch (wire_err) {
    case FIO_EPERM:   return EPERM;
    case FIO_ENOENT:  return ENOENT;
    case FIO_EIO:     return EIO;
    case FIO_EACCES:  return EACCES;
    case FIO_EEXIST:  return EEXIST;
    case FIO_ENOTDIR: return ENOTDIR;
    case FIO_EISDIR:  return EISDIR;
    case FIO_EINVAL:  return EINVAL;
    case FIO_ENOSPC:  return ENOSPC;
#ifdef EROFS
    case FIO_EROFS:   return EROFS;
#endif
    case FIO_ERANGE:  return ERANGE;
    default:          return EIO;
    }
}

/* ======================================================================
 * E. Encoding helpers for each request type
 * ====================================================================== */

/* All encode functions return the number of bytes written, or -1 on error.
 * When FIO_TESTING is defined, these are externally visible for tests. */

#ifdef FIO_TESTING
#define FIO_STATIC
#else
#define FIO_STATIC static
#endif

/* --- Request encoders --- */

/* OpenRequest: req_id(2) + file_id(2) + flags(4) + mode(2) + path(variable) */
FIO_STATIC int encode_open_req(uint8_t *buf, uint32_t cap,
                               uint16_t req_id, uint16_t file_id,
                               uint32_t flags, uint16_t mode,
                               const char *path) {
    size_t raw_len = strlen(path);
    if (raw_len > FIO_PATH_MAX) return -1;
    uint32_t path_len = (uint32_t)raw_len;
    uint32_t need = 10 + path_len;
    if (cap < need) return -1;
    put_u16(buf, req_id);
    put_u16(buf + 2, file_id);
    put_u32(buf + 4, flags);
    put_u16(buf + 8, mode);
    memcpy(buf + 10, path, path_len);
    return (int)need;
}

FIO_STATIC int decode_open_req(const uint8_t *buf, uint32_t len,
                               uint16_t *req_id, uint16_t *file_id,
                               uint32_t *flags, uint16_t *mode,
                               char *path, uint32_t path_cap) {
    if (len < 10) return -1;
    *req_id  = get_u16(buf);
    *file_id = get_u16(buf + 2);
    *flags   = get_u32(buf + 4);
    *mode    = get_u16(buf + 8);
    uint32_t plen = len - 10;
    if (plen >= path_cap) return -1;
    memcpy(path, buf + 10, plen);
    path[plen] = '\0';
    return 0;
}

/* ReadRequest: req_id(2) + file_id(2) + nbytes(4) */
FIO_STATIC int encode_read_req(uint8_t *buf, uint32_t cap,
                               uint16_t req_id, uint16_t file_id,
                               uint32_t nbytes) {
    if (cap < 8) return -1;
    put_u16(buf, req_id);
    put_u16(buf + 2, file_id);
    put_u32(buf + 4, nbytes);
    return 8;
}

FIO_STATIC int decode_read_req(const uint8_t *buf, uint32_t len,
                               uint16_t *req_id, uint16_t *file_id,
                               uint32_t *nbytes) {
    if (len < 8) return -1;
    *req_id  = get_u16(buf);
    *file_id = get_u16(buf + 2);
    *nbytes  = get_u32(buf + 4);
    return 0;
}

/* WriteRequest: req_id(2) + file_id(2) + data(variable) */
FIO_STATIC int encode_write_req(uint8_t *buf, uint32_t cap,
                                uint16_t req_id, uint16_t file_id,
                                const uint8_t *data, uint32_t data_len) {
    uint32_t need = 4 + data_len;
    if (cap < need) return -1;
    put_u16(buf, req_id);
    put_u16(buf + 2, file_id);
    if (data_len > 0)
        memcpy(buf + 4, data, data_len);
    return (int)need;
}

FIO_STATIC int decode_write_req(const uint8_t *buf, uint32_t len,
                                uint16_t *req_id, uint16_t *file_id,
                                const uint8_t **data, uint32_t *data_len) {
    if (len < 4) return -1;
    *req_id   = get_u16(buf);
    *file_id  = get_u16(buf + 2);
    *data     = buf + 4;
    *data_len = len - 4;
    return 0;
}

/* SeekRequest: req_id(2) + file_id(2) + offset(8) + whence(1) = 13 */
FIO_STATIC int encode_seek_req(uint8_t *buf, uint32_t cap,
                               uint16_t req_id, uint16_t file_id,
                               int64_t offset, uint8_t whence) {
    if (cap < 13) return -1;
    put_u16(buf, req_id);
    put_u16(buf + 2, file_id);
    put_u64(buf + 4, (uint64_t)offset);
    buf[12] = whence;
    return 13;
}

FIO_STATIC int decode_seek_req(const uint8_t *buf, uint32_t len,
                               uint16_t *req_id, uint16_t *file_id,
                               int64_t *offset, uint8_t *whence) {
    if (len < 13) return -1;
    *req_id  = get_u16(buf);
    *file_id = get_u16(buf + 2);
    *offset  = (int64_t)get_u64(buf + 4);
    *whence  = buf[12];
    return 0;
}

/* CloseRequest: req_id(2) + file_id(2) = 4 */
FIO_STATIC int encode_close_req(uint8_t *buf, uint32_t cap,
                                uint16_t req_id, uint16_t file_id) {
    if (cap < 4) return -1;
    put_u16(buf, req_id);
    put_u16(buf + 2, file_id);
    return 4;
}

FIO_STATIC int decode_close_req(const uint8_t *buf, uint32_t len,
                                uint16_t *req_id, uint16_t *file_id) {
    if (len < 4) return -1;
    *req_id  = get_u16(buf);
    *file_id = get_u16(buf + 2);
    return 0;
}

/* FstatRequest: req_id(2) + file_id(2) = 4 */
FIO_STATIC int encode_fstat_req(uint8_t *buf, uint32_t cap,
                                uint16_t req_id, uint16_t file_id) {
    if (cap < 4) return -1;
    put_u16(buf, req_id);
    put_u16(buf + 2, file_id);
    return 4;
}

FIO_STATIC int decode_fstat_req(const uint8_t *buf, uint32_t len,
                                uint16_t *req_id, uint16_t *file_id) {
    if (len < 4) return -1;
    *req_id  = get_u16(buf);
    *file_id = get_u16(buf + 2);
    return 0;
}

/* FtruncateRequest: req_id(2) + file_id(2) + length(8) = 12 */
FIO_STATIC int encode_ftruncate_req(uint8_t *buf, uint32_t cap,
                                    uint16_t req_id, uint16_t file_id,
                                    int64_t length) {
    if (cap < 12) return -1;
    put_u16(buf, req_id);
    put_u16(buf + 2, file_id);
    put_u64(buf + 4, (uint64_t)length);
    return 12;
}

FIO_STATIC int decode_ftruncate_req(const uint8_t *buf, uint32_t len,
                                    uint16_t *req_id, uint16_t *file_id,
                                    int64_t *length) {
    if (len < 12) return -1;
    *req_id  = get_u16(buf);
    *file_id = get_u16(buf + 2);
    *length  = (int64_t)get_u64(buf + 4);
    return 0;
}

/* UnlinkRequest: req_id(2) + path(variable) */
FIO_STATIC int encode_unlink_req(uint8_t *buf, uint32_t cap,
                                 uint16_t req_id, const char *path) {
    size_t raw_len = strlen(path);
    if (raw_len > FIO_PATH_MAX) return -1;
    uint32_t path_len = (uint32_t)raw_len;
    uint32_t need = 2 + path_len;
    if (cap < need) return -1;
    put_u16(buf, req_id);
    memcpy(buf + 2, path, path_len);
    return (int)need;
}

FIO_STATIC int decode_unlink_req(const uint8_t *buf, uint32_t len,
                                 uint16_t *req_id,
                                 char *path, uint32_t path_cap) {
    if (len < 2) return -1;
    *req_id = get_u16(buf);
    uint32_t plen = len - 2;
    if (plen >= path_cap) return -1;
    memcpy(path, buf + 2, plen);
    path[plen] = '\0';
    return 0;
}

/* RenameRequest: req_id(2) + old_len(2) + old(variable) + new(variable) */
FIO_STATIC int encode_rename_req(uint8_t *buf, uint32_t cap,
                                 uint16_t req_id,
                                 const char *old_path, const char *new_path) {
    size_t raw_old = strlen(old_path);
    size_t raw_new = strlen(new_path);
    if (raw_old > FIO_PATH_MAX || raw_new > FIO_PATH_MAX) return -1;
    uint32_t old_len = (uint32_t)raw_old;
    uint32_t new_len = (uint32_t)raw_new;
    uint32_t need = 4 + old_len + new_len;
    if (cap < need) return -1;
    put_u16(buf, req_id);
    put_u16(buf + 2, (uint16_t)old_len);
    memcpy(buf + 4, old_path, old_len);
    memcpy(buf + 4 + old_len, new_path, new_len);
    return (int)need;
}

FIO_STATIC int decode_rename_req(const uint8_t *buf, uint32_t len,
                                 uint16_t *req_id,
                                 char *old_path, uint32_t old_cap,
                                 char *new_path, uint32_t new_cap) {
    if (len < 4) return -1;
    *req_id = get_u16(buf);
    uint16_t old_len = get_u16(buf + 2);
    if (len < (uint32_t)(4 + old_len)) return -1;
    uint32_t new_len = len - 4 - old_len;
    if (old_len >= old_cap || new_len >= new_cap) return -1;
    memcpy(old_path, buf + 4, old_len);
    old_path[old_len] = '\0';
    memcpy(new_path, buf + 4 + old_len, new_len);
    new_path[new_len] = '\0';
    return 0;
}

/* MkdirRequest: req_id(2) + mode(2) + path(variable) */
FIO_STATIC int encode_mkdir_req(uint8_t *buf, uint32_t cap,
                                uint16_t req_id, uint16_t mode,
                                const char *path) {
    size_t raw_len = strlen(path);
    if (raw_len > FIO_PATH_MAX) return -1;
    uint32_t path_len = (uint32_t)raw_len;
    uint32_t need = 4 + path_len;
    if (cap < need) return -1;
    put_u16(buf, req_id);
    put_u16(buf + 2, mode);
    memcpy(buf + 4, path, path_len);
    return (int)need;
}

FIO_STATIC int decode_mkdir_req(const uint8_t *buf, uint32_t len,
                                uint16_t *req_id, uint16_t *mode,
                                char *path, uint32_t path_cap) {
    if (len < 4) return -1;
    *req_id = get_u16(buf);
    *mode   = get_u16(buf + 2);
    uint32_t plen = len - 4;
    if (plen >= path_cap) return -1;
    memcpy(path, buf + 4, plen);
    path[plen] = '\0';
    return 0;
}

/* --- Response encoders/decoders --- */

/* OpenOkResponse: req_id(2) + file_size(8) = 10 */
FIO_STATIC int encode_open_ok(uint8_t *buf, uint32_t cap,
                              uint16_t req_id, int64_t file_size) {
    if (cap < 10) return -1;
    put_u16(buf, req_id);
    put_u64(buf + 2, (uint64_t)file_size);
    return 10;
}

FIO_STATIC int decode_open_ok(const uint8_t *buf, uint32_t len,
                              uint16_t *req_id, int64_t *file_size) {
    if (len < 10) return -1;
    *req_id    = get_u16(buf);
    *file_size = (int64_t)get_u64(buf + 2);
    return 0;
}

/* ReadOkResponse: req_id(2) + data(variable) */
FIO_STATIC int encode_read_ok(uint8_t *buf, uint32_t cap,
                              uint16_t req_id,
                              const uint8_t *data, uint32_t data_len) {
    uint32_t need = 2 + data_len;
    if (cap < need) return -1;
    put_u16(buf, req_id);
    if (data_len > 0)
        memcpy(buf + 2, data, data_len);
    return (int)need;
}

FIO_STATIC int decode_read_ok(const uint8_t *buf, uint32_t len,
                              uint16_t *req_id,
                              const uint8_t **data, uint32_t *data_len) {
    if (len < 2) return -1;
    *req_id   = get_u16(buf);
    *data     = buf + 2;
    *data_len = len - 2;
    return 0;
}

/* WriteOkResponse: req_id(2) + bytes_written(4) = 6 */
FIO_STATIC int encode_write_ok(uint8_t *buf, uint32_t cap,
                               uint16_t req_id, uint32_t bytes_written) {
    if (cap < 6) return -1;
    put_u16(buf, req_id);
    put_u32(buf + 2, bytes_written);
    return 6;
}

FIO_STATIC int decode_write_ok(const uint8_t *buf, uint32_t len,
                               uint16_t *req_id, uint32_t *bytes_written) {
    if (len < 6) return -1;
    *req_id        = get_u16(buf);
    *bytes_written = get_u32(buf + 2);
    return 0;
}

/* SeekOkResponse: req_id(2) + offset(8) = 10 */
FIO_STATIC int encode_seek_ok(uint8_t *buf, uint32_t cap,
                              uint16_t req_id, int64_t offset) {
    if (cap < 10) return -1;
    put_u16(buf, req_id);
    put_u64(buf + 2, (uint64_t)offset);
    return 10;
}

FIO_STATIC int decode_seek_ok(const uint8_t *buf, uint32_t len,
                              uint16_t *req_id, int64_t *offset) {
    if (len < 10) return -1;
    *req_id = get_u16(buf);
    *offset = (int64_t)get_u64(buf + 2);
    return 0;
}

/* RequestIDResponse (CloseOk, FtruncateOk, UnlinkOk, RenameOk, MkdirOk): req_id(2) */
FIO_STATIC int encode_reqid_resp(uint8_t *buf, uint32_t cap, uint16_t req_id) {
    if (cap < 2) return -1;
    put_u16(buf, req_id);
    return 2;
}

FIO_STATIC int decode_reqid_resp(const uint8_t *buf, uint32_t len,
                                 uint16_t *req_id) {
    if (len < 2) return -1;
    *req_id = get_u16(buf);
    return 0;
}

/* FstatOkResponse: req_id(2) + file_size(8) + mode(4) = 14 */
FIO_STATIC int encode_fstat_ok(uint8_t *buf, uint32_t cap,
                               uint16_t req_id, int64_t file_size,
                               uint32_t mode) {
    if (cap < 14) return -1;
    put_u16(buf, req_id);
    put_u64(buf + 2, (uint64_t)file_size);
    put_u32(buf + 10, mode);
    return 14;
}

FIO_STATIC int decode_fstat_ok(const uint8_t *buf, uint32_t len,
                               uint16_t *req_id, int64_t *file_size,
                               uint32_t *mode) {
    if (len < 14) return -1;
    *req_id    = get_u16(buf);
    *file_size = (int64_t)get_u64(buf + 2);
    *mode      = get_u32(buf + 10);
    return 0;
}

/* IoErrorResponse: req_id(2) + errno(4) = 6 */
FIO_STATIC int encode_io_error(uint8_t *buf, uint32_t cap,
                               uint16_t req_id, int32_t err) {
    if (cap < 6) return -1;
    put_u16(buf, req_id);
    put_u32(buf + 2, (uint32_t)err);
    return 6;
}

FIO_STATIC int decode_io_error(const uint8_t *buf, uint32_t len,
                               uint16_t *req_id, int32_t *err) {
    if (len < 6) return -1;
    *req_id = get_u16(buf);
    *err    = (int32_t)get_u32(buf + 2);
    return 0;
}

/* ======================================================================
 * F. Socket I/O helpers
 * ====================================================================== */

/* Write exactly n bytes to fd. Returns 0 on success, -1 on error. */
static int write_all(int fd, const uint8_t *buf, size_t n) {
    while (n > 0) {
#ifdef _WIN32
        int w = send(fd, (const char *)buf, (int)n, 0);
#else
        ssize_t w = write(fd, buf, n);
#endif
        if (w <= 0) return -1;
        buf += w;
        n -= (size_t)w;
    }
    return 0;
}

/* Read exactly n bytes from fd. Returns 0 on success, -1 on error. */
static int read_all(int fd, uint8_t *buf, size_t n) {
    while (n > 0) {
#ifdef _WIN32
        int r = recv(fd, (char *)buf, (int)n, 0);
#else
        ssize_t r = read(fd, buf, n);
#endif
        if (r <= 0) return -1;
        buf += r;
        n -= (size_t)r;
    }
    return 0;
}

/* Send an envelope on the socket. Caller must hold send_mutex. */
static int send_envelope(uint8_t type, const uint8_t *payload, uint32_t payload_len) {
    uint8_t header[5];
    header[0] = type;
    put_u32(header + 1, payload_len);
    if (write_all(fio_state.sock_fd, header, 5) < 0) return -1;
    if (payload_len > 0 && write_all(fio_state.sock_fd, payload, payload_len) < 0) return -1;
    return 0;
}

/* ======================================================================
 * G. Response Dispatching
 * ====================================================================== */

/* Extract req_id from any response payload (always first 2 bytes) */
static uint16_t extract_req_id(const uint8_t *payload, uint32_t len) {
    if (len < 2) return 0;
    return get_u16(payload);
}

/* Reader thread: loops reading envelopes, depositing into pending slots */
static void *reader_thread_func(void *arg) {
    (void)arg;
    for (;;) {
        /* Read envelope header */
        uint8_t header[5];
        if (read_all(fio_state.sock_fd, header, 5) < 0) break;

        uint8_t type = header[0];
        uint32_t plen = get_u32(header + 1);

        uint8_t *payload = NULL;
        if (plen > 0) {
            payload = malloc(plen);
            if (!payload) break;
            if (read_all(fio_state.sock_fd, payload, plen) < 0) {
                free(payload);
                break;
            }
        }

        uint16_t req_id = extract_req_id(payload, plen);

        pthread_mutex_lock(&fio_state.dispatch_mutex);
        for (int i = 0; i < FIO_MAX_PENDING; i++) {
            if (fio_state.pending[i].used && fio_state.pending[i].req_id == req_id) {
                fio_state.pending[i].resp_type    = type;
                fio_state.pending[i].resp_payload = payload;
                fio_state.pending[i].resp_len     = plen;
                payload = NULL; /* ownership transferred */
                break;
            }
        }
        pthread_cond_broadcast(&fio_state.dispatch_cond);
        pthread_mutex_unlock(&fio_state.dispatch_mutex);

        free(payload); /* free if no slot claimed it */
    }
    return NULL;
}

/* ======================================================================
 * H. send_and_wait Helper
 * ====================================================================== */

/* Allocate a pending slot. Returns slot index or -1. Caller must hold dispatch_mutex. */
static int alloc_pending(uint16_t req_id) {
    for (int i = 0; i < FIO_MAX_PENDING; i++) {
        if (!fio_state.pending[i].used) {
            fio_state.pending[i].used         = 1;
            fio_state.pending[i].req_id       = req_id;
            fio_state.pending[i].resp_type    = 0;
            fio_state.pending[i].resp_payload = NULL;
            fio_state.pending[i].resp_len     = 0;
            return i;
        }
    }
    return -1;
}

/* Send request and wait for response. Returns slot index with response populated.
 * On error returns -1 and sets errno. Caller must free resp_payload after use. */
static int send_and_wait(uint8_t msg_type, const uint8_t *payload, uint32_t payload_len,
                         uint16_t req_id) {
    /* Allocate pending slot */
    pthread_mutex_lock(&fio_state.dispatch_mutex);
    int slot = alloc_pending(req_id);
    pthread_mutex_unlock(&fio_state.dispatch_mutex);
    if (slot < 0) {
        errno = ENOMEM;
        return -1;
    }

    /* Send request */
    pthread_mutex_lock(&fio_state.send_mutex);
    int rc = send_envelope(msg_type, payload, payload_len);
    pthread_mutex_unlock(&fio_state.send_mutex);
    if (rc < 0) {
        pthread_mutex_lock(&fio_state.dispatch_mutex);
        fio_state.pending[slot].used = 0;
        pthread_mutex_unlock(&fio_state.dispatch_mutex);
        errno = EIO;
        return -1;
    }

    /* Wait for response */
    pthread_mutex_lock(&fio_state.dispatch_mutex);
    while (fio_state.pending[slot].resp_payload == NULL && fio_state.pending[slot].resp_type == 0) {
        pthread_cond_wait(&fio_state.dispatch_cond, &fio_state.dispatch_mutex);
    }
    pthread_mutex_unlock(&fio_state.dispatch_mutex);

    return slot;
}

/* Free a pending slot after processing */
static void free_pending(int slot) {
    pthread_mutex_lock(&fio_state.dispatch_mutex);
    free(fio_state.pending[slot].resp_payload);
    fio_state.pending[slot].resp_payload = NULL;
    fio_state.pending[slot].used = 0;
    pthread_mutex_unlock(&fio_state.dispatch_mutex);
}

/* ======================================================================
 * I. Virtual FD Table
 * ====================================================================== */

static int vfd_alloc(uint16_t file_id, int64_t initial_size) {
    for (int i = 0; i < FIO_MAX_FILES; i++) {
        if (!fio_state.vfds[i].active) {
            fio_state.vfds[i].active      = 1;
            fio_state.vfds[i].file_id     = file_id;
            fio_state.vfds[i].cached_size = initial_size;
            fio_state.vfds[i].dirty       = 0;
            return FIO_VFD_BASE + i;
        }
    }
    return -1;
}

static fio_vfd_t *vfd_get(int fd) {
    if (fd < FIO_VFD_BASE || fd >= FIO_VFD_BASE + FIO_MAX_FILES) return NULL;
    int idx = fd - FIO_VFD_BASE;
    if (!fio_state.vfds[idx].active) return NULL;
    return &fio_state.vfds[idx];
}

/* Real kernel fds (stdio, dup'd stdio, fd: protocol, etc.) live below
 * FIO_VFD_BASE. Virtual fds returned by fio_open start at FIO_VFD_BASE.
 * Anything below that range didn't come from our tunnel — ffmpeg got it
 * from the kernel (pipe: protocol dups stdin/stdout into a fresh fd; fd:
 * takes an fd directly), so read/write/seek/close/fstat on it must go to
 * real syscalls. Server-side Go plumbing already forwards stdio to/from
 * the client (MsgStdin/MsgStdout/MsgStderr). */
static inline int is_real_fd(int fd) {
    return fd >= 0 && fd < FIO_VFD_BASE;
}

static void vfd_free(int fd) {
    if (fd < FIO_VFD_BASE || fd >= FIO_VFD_BASE + FIO_MAX_FILES) return;
    fio_state.vfds[fd - FIO_VFD_BASE].active = 0;
}

/* ======================================================================
 * J. Lazy Init
 * ====================================================================== */

static void fio_init(void) {
    memset(&fio_state, 0, sizeof(fio_state));
    fio_state.sock_fd = -1;
    fio_state.next_file_id = 1;
    fio_state.next_req_id = 1;
    pthread_mutex_init(&fio_state.send_mutex, NULL);
    pthread_mutex_init(&fio_state.dispatch_mutex, NULL);
    pthread_cond_init(&fio_state.dispatch_cond, NULL);

    const char *port_str = getenv("FFOIP_PORT");
    if (!port_str || port_str[0] == '\0') {
        fio_state.initialized = 1; /* passthrough */
        return;
    }

    int port = atoi(port_str);
    if (port <= 0 || port > 65535) {
        fprintf(stderr, "fio: invalid FFOIP_PORT=%s, falling back to passthrough\n", port_str);
        fio_state.initialized = 1;
        return;
    }

    /* Connect to 127.0.0.1:<port> */
#ifdef _WIN32
    WSADATA wsa;
    if (WSAStartup(MAKEWORD(2, 2), &wsa) != 0) {
        fprintf(stderr, "fio: WSAStartup failed\n");
        fio_state.initialized = 1;
        return;
    }
#endif

    int sock = socket(AF_INET, SOCK_STREAM, 0);
    if (sock < 0) {
#ifdef _WIN32
        fprintf(stderr, "fio: socket() failed: WSA error %d\n", WSAGetLastError());
#else
        fprintf(stderr, "fio: socket() failed: %s\n", strerror(errno));
#endif
        fio_state.initialized = 1;
        return;
    }

    struct sockaddr_in addr;
    memset(&addr, 0, sizeof(addr));
    addr.sin_family = AF_INET;
    addr.sin_port = htons((uint16_t)port);
    addr.sin_addr.s_addr = htonl(0x7f000001); /* 127.0.0.1 */

    if (connect(sock, (struct sockaddr *)&addr, sizeof(addr)) < 0) {
#ifdef _WIN32
        fprintf(stderr, "fio: connect to 127.0.0.1:%d failed: WSA error %d\n", port, WSAGetLastError());
#else
        fprintf(stderr, "fio: connect to 127.0.0.1:%d failed: %s\n", port, strerror(errno));
#endif
#ifdef _WIN32
        closesocket(sock);
#else
        close(sock);
#endif
        fio_state.initialized = 1;
        return;
    }

    fio_state.sock_fd = sock;

    /* Start reader thread */
    if (pthread_create(&fio_state.reader_thread, NULL, reader_thread_func, NULL) != 0) {
#ifdef _WIN32
        closesocket(sock);
#else
        close(sock);
#endif
        fio_state.sock_fd = -1;
        fio_state.initialized = 1; /* passthrough */
        return;
    }

    fio_state.initialized = 2; /* tunneled */
}

static void fio_ensure_init(void) {
    pthread_once(&fio_once, fio_init);
}

/* ======================================================================
 * K. Public API Functions
 * ====================================================================== */

int fio_open(const char *path, int flags, mode_t mode) {
    fio_ensure_init();

    if (fio_state.initialized == 1) {
        return open(path, flags, mode);
    }

    uint32_t wire_flags = flags_to_wire(flags);
    uint16_t wire_mode = (uint16_t)(mode & 0xFFFF);

    pthread_mutex_lock(&fio_state.send_mutex);
    uint16_t req_id  = fio_state.next_req_id++;
    uint16_t file_id = fio_state.next_file_id++;
    pthread_mutex_unlock(&fio_state.send_mutex);

    uint8_t buf[4096];
    int n = encode_open_req(buf, sizeof(buf), req_id, file_id, wire_flags, wire_mode, path);
    if (n < 0) { errno = ENAMETOOLONG; return -1; }

    int slot = send_and_wait(FIO_MSG_OPEN, buf, (uint32_t)n, req_id);
    if (slot < 0) return -1;

    int result;
    if (fio_state.pending[slot].resp_type == FIO_MSG_IO_ERROR) {
        int32_t wire_err = FIO_EIO;
        decode_io_error(fio_state.pending[slot].resp_payload,
                        fio_state.pending[slot].resp_len, &(uint16_t){0}, &wire_err);
        errno = errno_from_wire(wire_err);
        result = -1;
    } else if (fio_state.pending[slot].resp_type == FIO_MSG_OPEN_OK) {
        int64_t file_size = 0;
        if (decode_open_ok(fio_state.pending[slot].resp_payload,
                           fio_state.pending[slot].resp_len, &(uint16_t){0}, &file_size) < 0) {
            errno = EIO;
            result = -1;
        } else {
            result = vfd_alloc(file_id, file_size);
            if (result < 0) { errno = ENOMEM; result = -1; }
        }
    } else {
        errno = EIO;
        result = -1;
    }

    free_pending(slot);
    return result;
}

ssize_t fio_read(int fd, void *buf, size_t count) {
    fio_ensure_init();

    if (fio_state.initialized == 1 || is_real_fd(fd)) {
        return read(fd, buf, count);
    }

    fio_vfd_t *vfd = vfd_get(fd);
    if (!vfd) { errno = EBADF; return -1; }

    pthread_mutex_lock(&fio_state.send_mutex);
    uint16_t req_id = fio_state.next_req_id++;
    pthread_mutex_unlock(&fio_state.send_mutex);

    uint8_t req_buf[8];
    encode_read_req(req_buf, sizeof(req_buf), req_id, vfd->file_id,
                    (uint32_t)(count > 0xFFFFFFFF ? 0xFFFFFFFF : count));

    int slot = send_and_wait(FIO_MSG_READ, req_buf, 8, req_id);
    if (slot < 0) return -1;

    ssize_t result;
    if (fio_state.pending[slot].resp_type == FIO_MSG_IO_ERROR) {
        int32_t wire_err = FIO_EIO;
        decode_io_error(fio_state.pending[slot].resp_payload,
                        fio_state.pending[slot].resp_len, &(uint16_t){0}, &wire_err);
        errno = errno_from_wire(wire_err);
        result = -1;
    } else if (fio_state.pending[slot].resp_type == FIO_MSG_READ_OK) {
        const uint8_t *data = NULL;
        uint32_t data_len = 0;
        if (decode_read_ok(fio_state.pending[slot].resp_payload,
                           fio_state.pending[slot].resp_len, &(uint16_t){0}, &data, &data_len) < 0) {
            errno = EIO;
            result = -1;
        } else {
            if (data_len > count) data_len = (uint32_t)count;
            memcpy(buf, data, data_len);
            result = (ssize_t)data_len;
        }
    } else {
        errno = EIO;
        result = -1;
    }

    free_pending(slot);
    return result;
}

ssize_t fio_write(int fd, const void *buf, size_t count) {
    fio_ensure_init();

    if (fio_state.initialized == 1 || is_real_fd(fd)) {
        return write(fd, buf, count);
    }

    fio_vfd_t *vfd = vfd_get(fd);
    if (!vfd) { errno = EBADF; return -1; }

    pthread_mutex_lock(&fio_state.send_mutex);
    uint16_t req_id = fio_state.next_req_id++;
    pthread_mutex_unlock(&fio_state.send_mutex);

    uint32_t payload_len = 4 + (uint32_t)count;
    uint8_t *req_buf = malloc(payload_len);
    if (!req_buf) { errno = ENOMEM; return -1; }

    encode_write_req(req_buf, payload_len, req_id, vfd->file_id,
                     (const uint8_t *)buf, (uint32_t)count);

    int slot = send_and_wait(FIO_MSG_WRITE, req_buf, payload_len, req_id);
    free(req_buf);
    if (slot < 0) return -1;

    ssize_t result;
    if (fio_state.pending[slot].resp_type == FIO_MSG_IO_ERROR) {
        int32_t wire_err = FIO_EIO;
        decode_io_error(fio_state.pending[slot].resp_payload,
                        fio_state.pending[slot].resp_len, &(uint16_t){0}, &wire_err);
        errno = errno_from_wire(wire_err);
        result = -1;
    } else if (fio_state.pending[slot].resp_type == FIO_MSG_WRITE_OK) {
        uint32_t written = 0;
        if (decode_write_ok(fio_state.pending[slot].resp_payload,
                            fio_state.pending[slot].resp_len, &(uint16_t){0}, &written) < 0) {
            errno = EIO;
            result = -1;
        } else {
            vfd->dirty = 1;
            result = (ssize_t)written;
        }
    } else {
        errno = EIO;
        result = -1;
    }

    free_pending(slot);
    return result;
}

off_t fio_lseek(int fd, off_t offset, int whence) {
    fio_ensure_init();

    if (fio_state.initialized == 1 || is_real_fd(fd)) {
#ifdef _WIN32
        return _lseeki64(fd, offset, whence);
#else
        return lseek(fd, offset, whence);
#endif
    }

    fio_vfd_t *vfd = vfd_get(fd);
    if (!vfd) { errno = EBADF; return -1; }

    uint8_t wire_whence;
    switch (whence) {
    case SEEK_SET: wire_whence = FIO_SEEK_SET; break;
    case SEEK_CUR: wire_whence = FIO_SEEK_CUR; break;
    case SEEK_END: wire_whence = FIO_SEEK_END; break;
    default: errno = EINVAL; return -1;
    }

    pthread_mutex_lock(&fio_state.send_mutex);
    uint16_t req_id = fio_state.next_req_id++;
    pthread_mutex_unlock(&fio_state.send_mutex);

    uint8_t req_buf[13];
    encode_seek_req(req_buf, sizeof(req_buf), req_id, vfd->file_id,
                    (int64_t)offset, wire_whence);

    int slot = send_and_wait(FIO_MSG_SEEK, req_buf, 13, req_id);
    if (slot < 0) return -1;

    off_t result;
    if (fio_state.pending[slot].resp_type == FIO_MSG_IO_ERROR) {
        int32_t wire_err = FIO_EIO;
        decode_io_error(fio_state.pending[slot].resp_payload,
                        fio_state.pending[slot].resp_len, &(uint16_t){0}, &wire_err);
        errno = errno_from_wire(wire_err);
        result = -1;
    } else if (fio_state.pending[slot].resp_type == FIO_MSG_SEEK_OK) {
        int64_t new_off = 0;
        if (decode_seek_ok(fio_state.pending[slot].resp_payload,
                           fio_state.pending[slot].resp_len, &(uint16_t){0}, &new_off) < 0) {
            errno = EIO;
            result = -1;
        } else {
            result = (off_t)new_off;
        }
    } else {
        errno = EIO;
        result = -1;
    }

    free_pending(slot);
    return result;
}

int fio_close(int fd) {
    fio_ensure_init();

    if (fio_state.initialized == 1 || is_real_fd(fd)) {
        return close(fd);
    }

    fio_vfd_t *vfd = vfd_get(fd);
    if (!vfd) { errno = EBADF; return -1; }

    pthread_mutex_lock(&fio_state.send_mutex);
    uint16_t req_id = fio_state.next_req_id++;
    pthread_mutex_unlock(&fio_state.send_mutex);

    uint8_t req_buf[4];
    encode_close_req(req_buf, sizeof(req_buf), req_id, vfd->file_id);

    int slot = send_and_wait(FIO_MSG_CLOSE, req_buf, 4, req_id);
    if (slot < 0) return -1;

    int result;
    if (fio_state.pending[slot].resp_type == FIO_MSG_IO_ERROR) {
        int32_t wire_err = FIO_EIO;
        decode_io_error(fio_state.pending[slot].resp_payload,
                        fio_state.pending[slot].resp_len, &(uint16_t){0}, &wire_err);
        errno = errno_from_wire(wire_err);
        result = -1;
    } else {
        result = 0;
    }

    vfd_free(fd);
    free_pending(slot);
    return result;
}

int fio_fstat(int fd, struct stat *st) {
    fio_ensure_init();

    if (fio_state.initialized == 1 || is_real_fd(fd)) {
        return fstat(fd, st);
    }

    fio_vfd_t *vfd = vfd_get(fd);
    if (!vfd) { errno = EBADF; return -1; }

    /* Return cached size if not dirty */
    if (!vfd->dirty) {
        memset(st, 0, sizeof(*st));
        st->st_size = vfd->cached_size;
        st->st_mode = S_IFREG | 0644;
        return 0;
    }

    pthread_mutex_lock(&fio_state.send_mutex);
    uint16_t req_id = fio_state.next_req_id++;
    pthread_mutex_unlock(&fio_state.send_mutex);

    uint8_t req_buf[4];
    encode_fstat_req(req_buf, sizeof(req_buf), req_id, vfd->file_id);

    int slot = send_and_wait(FIO_MSG_FSTAT, req_buf, 4, req_id);
    if (slot < 0) return -1;

    int result;
    if (fio_state.pending[slot].resp_type == FIO_MSG_IO_ERROR) {
        int32_t wire_err = FIO_EIO;
        decode_io_error(fio_state.pending[slot].resp_payload,
                        fio_state.pending[slot].resp_len, &(uint16_t){0}, &wire_err);
        errno = errno_from_wire(wire_err);
        result = -1;
    } else if (fio_state.pending[slot].resp_type == FIO_MSG_FSTAT_OK) {
        int64_t file_size = 0;
        uint32_t mode = 0;
        if (decode_fstat_ok(fio_state.pending[slot].resp_payload,
                            fio_state.pending[slot].resp_len, &(uint16_t){0},
                            &file_size, &mode) < 0) {
            errno = EIO;
            result = -1;
        } else {
            memset(st, 0, sizeof(*st));
            st->st_size = file_size;
            st->st_mode = mode;
            vfd->cached_size = file_size;
            vfd->dirty = 0;
            result = 0;
        }
    } else {
        errno = EIO;
        result = -1;
    }

    free_pending(slot);
    return result;
}

int fio_ftruncate(int fd, off_t length) {
    fio_ensure_init();

    if (fio_state.initialized == 1 || is_real_fd(fd)) {
#ifdef _WIN32
        return _chsize_s(fd, length) == 0 ? 0 : -1;
#else
        return ftruncate(fd, length);
#endif
    }

    fio_vfd_t *vfd = vfd_get(fd);
    if (!vfd) { errno = EBADF; return -1; }

    pthread_mutex_lock(&fio_state.send_mutex);
    uint16_t req_id = fio_state.next_req_id++;
    pthread_mutex_unlock(&fio_state.send_mutex);

    uint8_t req_buf[12];
    encode_ftruncate_req(req_buf, sizeof(req_buf), req_id, vfd->file_id, (int64_t)length);

    int slot = send_and_wait(FIO_MSG_FTRUNCATE, req_buf, 12, req_id);
    if (slot < 0) return -1;

    int result;
    if (fio_state.pending[slot].resp_type == FIO_MSG_IO_ERROR) {
        int32_t wire_err = FIO_EIO;
        decode_io_error(fio_state.pending[slot].resp_payload,
                        fio_state.pending[slot].resp_len, &(uint16_t){0}, &wire_err);
        errno = errno_from_wire(wire_err);
        result = -1;
    } else {
        vfd->dirty = 1;
        result = 0;
    }

    free_pending(slot);
    return result;
}

int fio_unlink(const char *path) {
    fio_ensure_init();

    if (fio_state.initialized == 1) {
        return unlink(path);
    }

    pthread_mutex_lock(&fio_state.send_mutex);
    uint16_t req_id = fio_state.next_req_id++;
    pthread_mutex_unlock(&fio_state.send_mutex);

    uint8_t buf[4096];
    int n = encode_unlink_req(buf, sizeof(buf), req_id, path);
    if (n < 0) { errno = ENAMETOOLONG; return -1; }

    int slot = send_and_wait(FIO_MSG_UNLINK, buf, (uint32_t)n, req_id);
    if (slot < 0) return -1;

    int result;
    if (fio_state.pending[slot].resp_type == FIO_MSG_IO_ERROR) {
        int32_t wire_err = FIO_EIO;
        decode_io_error(fio_state.pending[slot].resp_payload,
                        fio_state.pending[slot].resp_len, &(uint16_t){0}, &wire_err);
        errno = errno_from_wire(wire_err);
        result = -1;
    } else {
        result = 0;
    }

    free_pending(slot);
    return result;
}

int fio_rename(const char *oldpath, const char *newpath) {
    fio_ensure_init();

    if (fio_state.initialized == 1) {
        return rename(oldpath, newpath);
    }

    pthread_mutex_lock(&fio_state.send_mutex);
    uint16_t req_id = fio_state.next_req_id++;
    pthread_mutex_unlock(&fio_state.send_mutex);

    uint8_t buf[8192];
    int n = encode_rename_req(buf, sizeof(buf), req_id, oldpath, newpath);
    if (n < 0) { errno = ENAMETOOLONG; return -1; }

    int slot = send_and_wait(FIO_MSG_RENAME, buf, (uint32_t)n, req_id);
    if (slot < 0) return -1;

    int result;
    if (fio_state.pending[slot].resp_type == FIO_MSG_IO_ERROR) {
        int32_t wire_err = FIO_EIO;
        decode_io_error(fio_state.pending[slot].resp_payload,
                        fio_state.pending[slot].resp_len, &(uint16_t){0}, &wire_err);
        errno = errno_from_wire(wire_err);
        result = -1;
    } else {
        result = 0;
    }

    free_pending(slot);
    return result;
}

int fio_mkdir(const char *path, mode_t mode) {
    fio_ensure_init();

    if (fio_state.initialized == 1) {
#ifdef _WIN32
        (void)mode;
        return _mkdir(path);
#else
        return mkdir(path, mode);
#endif
    }

    pthread_mutex_lock(&fio_state.send_mutex);
    uint16_t req_id = fio_state.next_req_id++;
    pthread_mutex_unlock(&fio_state.send_mutex);

    uint8_t buf[4096];
    int n = encode_mkdir_req(buf, sizeof(buf), req_id, (uint16_t)(mode & 0xFFFF), path);
    if (n < 0) { errno = ENAMETOOLONG; return -1; }

    int slot = send_and_wait(FIO_MSG_MKDIR, buf, (uint32_t)n, req_id);
    if (slot < 0) return -1;

    int result;
    if (fio_state.pending[slot].resp_type == FIO_MSG_IO_ERROR) {
        int32_t wire_err = FIO_EIO;
        decode_io_error(fio_state.pending[slot].resp_payload,
                        fio_state.pending[slot].resp_len, &(uint16_t){0}, &wire_err);
        errno = errno_from_wire(wire_err);
        result = -1;
    } else {
        result = 0;
    }

    free_pending(slot);
    return result;
}
