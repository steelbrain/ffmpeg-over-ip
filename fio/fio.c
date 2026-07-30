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

/* Short-circuit: RO media (verified) and RW shared (full ops) for transcode cache.
 * RO: read-only opens under prefix may be served locally if verified.
 * RW: any open under prefix is local if contained — for shared /cache. */
#define FIO_MAX_PREFIXES     16
#define FIO_MAX_PREFIX_LEN   512
#ifndef O_ACCMODE
#define O_ACCMODE (O_RDONLY|O_WRONLY|O_RDWR)
#endif

/* Sequential read-ahead. This cuts request/response round trips for readers
 * like FFmpeg's AVIO layer that commonly pull 32 KiB at a time. */
#define FIO_INITIAL_READAHEAD_BYTES  (512 * 1024)
#define FIO_SMALL_FILE_READAHEAD_BYTES (1280 * 1024)
#define FIO_DEFAULT_READAHEAD_BYTES  (2 * 1024 * 1024)
#define FIO_MAX_READAHEAD_BYTES      (16 * 1024 * 1024)
#define FIO_LARGE_FILE_THRESHOLD     (1024LL * 1024LL * 1024LL)
#define FIO_RANGE_CACHE_FILE_LIMIT_BYTES (256LL * 1024LL * 1024LL)
#define FIO_RANGE_CACHE_MAX_BYTES        (256U * 1024U * 1024U)
#define FIO_SMALL_FILE_PREFETCH_PERCENT 50

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

typedef struct fio_cache_block {
    int64_t  start;
    uint8_t *payload;
    uint32_t data_offset;
    uint32_t len;
    struct fio_cache_block *next;
} fio_cache_block_t;

typedef struct {
    int      active;
    uint16_t file_id;
    uint32_t wire_flags;
    int64_t  cached_size;
    int64_t  logical_offset;
    int64_t  remote_offset;
    int64_t  read_cache_start;
    uint8_t *read_cache;
    uint32_t read_cache_data_offset;
    uint32_t read_cache_len;
    fio_cache_block_t *range_cache;
    uint32_t range_cache_bytes;
    uint32_t read_ahead_bytes;
    int      prefetch_slot;
    int64_t  prefetch_start;
    uint32_t prefetch_request_size;
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
    int               initialized;
    int               sock_fd;
    uint16_t          next_file_id;
    uint16_t          next_req_id;
    pthread_mutex_t   send_mutex;
    pthread_mutex_t   dispatch_mutex;
    pthread_cond_t    dispatch_cond;
    uint32_t          read_ahead_bytes;
    int               read_ahead_explicit;
    uint32_t          range_cache_max_bytes;
    char              local_prefixes[FIO_MAX_PREFIXES][FIO_MAX_PREFIX_LEN];
    int               local_prefix_count;
    char              shared_prefixes[FIO_MAX_PREFIXES][FIO_MAX_PREFIX_LEN];
    int               shared_prefix_count;
    uint64_t          sc_hits;
    uint64_t          sc_misses;
    uint64_t          sc_fallbacks;
    uint64_t          sc_verify_fail;
    uint64_t          sc_shared_hits;
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
static int send_async(uint8_t msg_type, const uint8_t *payload, uint32_t payload_len,
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

    return slot;
}

static void wait_pending_response(int slot) {
    pthread_mutex_lock(&fio_state.dispatch_mutex);
    while (fio_state.pending[slot].resp_payload == NULL && fio_state.pending[slot].resp_type == 0) {
        pthread_cond_wait(&fio_state.dispatch_cond, &fio_state.dispatch_mutex);
    }
    pthread_mutex_unlock(&fio_state.dispatch_mutex);
}

static int send_and_wait(uint8_t msg_type, const uint8_t *payload, uint32_t payload_len,
                         uint16_t req_id) {
    int slot = send_async(msg_type, payload, payload_len, req_id);
    if (slot < 0) return -1;
    wait_pending_response(slot);
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

static uint32_t vfd_initial_read_ahead(void);

static int vfd_alloc(uint16_t file_id, int64_t initial_size) {
    for (int i = 0; i < FIO_MAX_FILES; i++) {
        if (!fio_state.vfds[i].active) {
            fio_state.vfds[i].active           = 1;
            fio_state.vfds[i].file_id          = file_id;
            fio_state.vfds[i].wire_flags       = 0;
            fio_state.vfds[i].cached_size      = initial_size;
            fio_state.vfds[i].logical_offset   = 0;
            fio_state.vfds[i].remote_offset    = 0;
            fio_state.vfds[i].read_cache_start = 0;
            fio_state.vfds[i].read_cache       = NULL;
            fio_state.vfds[i].read_cache_data_offset = 0;
            fio_state.vfds[i].read_cache_len   = 0;
            fio_state.vfds[i].range_cache      = NULL;
            fio_state.vfds[i].range_cache_bytes = 0;
            fio_state.vfds[i].read_ahead_bytes = vfd_initial_read_ahead();
            fio_state.vfds[i].prefetch_slot    = -1;
            fio_state.vfds[i].prefetch_start   = 0;
            fio_state.vfds[i].prefetch_request_size = 0;
            fio_state.vfds[i].dirty            = 0;
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

static uint32_t vfd_initial_read_ahead(void) {
    if (fio_state.read_ahead_bytes == 0) return 0;
    if (fio_state.read_ahead_bytes < FIO_INITIAL_READAHEAD_BYTES) {
        return fio_state.read_ahead_bytes;
    }
    return FIO_INITIAL_READAHEAD_BYTES;
}

static uint32_t vfd_max_read_ahead(fio_vfd_t *vfd) {
    uint32_t max_read_ahead = fio_state.read_ahead_bytes;

    if (!fio_state.read_ahead_explicit &&
        vfd->cached_size >= 0 &&
        vfd->cached_size < FIO_LARGE_FILE_THRESHOLD &&
        max_read_ahead > FIO_SMALL_FILE_READAHEAD_BYTES) {
        max_read_ahead = FIO_SMALL_FILE_READAHEAD_BYTES;
    }

    return max_read_ahead;
}

static void vfd_reset_read_ahead(fio_vfd_t *vfd) {
    vfd->read_ahead_bytes = vfd_initial_read_ahead();
}

static void vfd_grow_read_ahead(fio_vfd_t *vfd) {
    uint32_t max_read_ahead = vfd_max_read_ahead(vfd);

    if (vfd->read_ahead_bytes == 0) return;
    if (vfd->read_ahead_bytes >= max_read_ahead) return;

    uint32_t next = vfd->read_ahead_bytes * 2;
    if (next < vfd->read_ahead_bytes) {
        next = max_read_ahead;
    }
    if (next > max_read_ahead) {
        next = max_read_ahead;
    }
    vfd->read_ahead_bytes = next;
}

static void vfd_invalidate_read_cache(fio_vfd_t *vfd) {
    free(vfd->read_cache);
    vfd->read_cache = NULL;
    vfd->read_cache_data_offset = 0;
    vfd->read_cache_len = 0;
    vfd->read_cache_start = vfd->logical_offset;

    fio_cache_block_t *block = vfd->range_cache;
    while (block) {
        fio_cache_block_t *next = block->next;
        free(block->payload);
        free(block);
        block = next;
    }
    vfd->range_cache = NULL;
    vfd->range_cache_bytes = 0;
}

static int vfd_should_retain_read_cache(fio_vfd_t *vfd) {
    uint32_t accmode = vfd->wire_flags & 0x0003;
    return accmode == FIO_O_RDONLY &&
           !vfd->dirty &&
           fio_state.range_cache_max_bytes > 0 &&
           vfd->cached_size >= 0 &&
           vfd->cached_size <= FIO_RANGE_CACHE_FILE_LIMIT_BYTES;
}

static int vfd_range_cache_covers(fio_vfd_t *vfd, int64_t start, uint32_t len) {
    if (len == 0) return 1;
    int64_t end = start + (int64_t)len;
    if (vfd->read_cache && start >= vfd->read_cache_start &&
        end <= vfd->read_cache_start + (int64_t)vfd->read_cache_len) {
        return 1;
    }
    for (fio_cache_block_t *block = vfd->range_cache; block; block = block->next) {
        if (start >= block->start && end <= block->start + (int64_t)block->len) {
            return 1;
        }
    }
    return 0;
}

static void vfd_evict_range_cache(fio_vfd_t *vfd) {
    while (vfd->range_cache && vfd->range_cache_bytes > fio_state.range_cache_max_bytes) {
        fio_cache_block_t *prev = NULL;
        fio_cache_block_t *block = vfd->range_cache;
        while (block->next) {
            prev = block;
            block = block->next;
        }

        if (prev) {
            prev->next = NULL;
        } else {
            vfd->range_cache = NULL;
        }
        vfd->range_cache_bytes -= block->len;
        free(block->payload);
        free(block);
    }
}

static int vfd_add_range_cache(fio_vfd_t *vfd, int64_t start, uint8_t *payload,
                               uint32_t data_offset, uint32_t len) {
    if (!payload || len == 0) return 0;
    if (!vfd_should_retain_read_cache(vfd)) return 0;
    if (vfd_range_cache_covers(vfd, start, len)) return 0;

    fio_cache_block_t *block = malloc(sizeof(*block));
    if (!block) return 0;
    block->start = start;
    block->payload = payload;
    block->data_offset = data_offset;
    block->len = len;
    block->next = vfd->range_cache;
    vfd->range_cache = block;
    vfd->range_cache_bytes += len;
    vfd_evict_range_cache(vfd);
    return 1;
}

static void vfd_retain_or_free_active_read_cache(fio_vfd_t *vfd) {
    uint8_t *payload = vfd->read_cache;
    int64_t start = vfd->read_cache_start;
    uint32_t data_offset = vfd->read_cache_data_offset;
    uint32_t len = vfd->read_cache_len;

    vfd->read_cache = NULL;
    vfd->read_cache_data_offset = 0;
    vfd->read_cache_len = 0;
    vfd->read_cache_start = vfd->logical_offset;

    if (!vfd_add_range_cache(vfd, start, payload, data_offset, len)) {
        free(payload);
    }
}

static void vfd_cancel_prefetch(fio_vfd_t *vfd);
static int vfd_take_prefetch_as_cache(fio_vfd_t *vfd);
static void vfd_maybe_start_prefetch(fio_vfd_t *vfd);

static void vfd_free(int fd) {
    if (fd < FIO_VFD_BASE || fd >= FIO_VFD_BASE + FIO_MAX_FILES) return;
    fio_vfd_t *vfd = &fio_state.vfds[fd - FIO_VFD_BASE];
    vfd_cancel_prefetch(vfd);
    vfd_invalidate_read_cache(vfd);
    vfd->active = 0;
}

static int vfd_cache_contains(fio_vfd_t *vfd, int64_t offset) {
    if (vfd->read_cache && vfd->read_cache_len > 0) {
        int64_t cache_end = vfd->read_cache_start + (int64_t)vfd->read_cache_len;
        if (offset >= vfd->read_cache_start && offset < cache_end) return 1;
    }
    for (fio_cache_block_t *block = vfd->range_cache; block; block = block->next) {
        int64_t cache_end = block->start + (int64_t)block->len;
        if (offset >= block->start && offset < cache_end) return 1;
    }
    return 0;
}

static int vfd_promote_range_cache_block(fio_vfd_t *vfd, fio_cache_block_t *prev,
                                         fio_cache_block_t *block) {
    if (!block) return 0;

    if (prev) {
        prev->next = block->next;
    } else {
        vfd->range_cache = block->next;
    }
    vfd->range_cache_bytes -= block->len;

    uint8_t *payload = block->payload;
    int64_t start = block->start;
    uint32_t data_offset = block->data_offset;
    uint32_t len = block->len;
    free(block);

    vfd_retain_or_free_active_read_cache(vfd);
    vfd->read_cache = payload;
    vfd->read_cache_data_offset = data_offset;
    vfd->read_cache_start = start;
    vfd->read_cache_len = len;
    return 1;
}

static ssize_t vfd_copy_from_cache(fio_vfd_t *vfd, void *buf, size_t count) {
retry_active:
    if (vfd->read_cache && vfd->read_cache_len > 0) {
        int64_t cache_end = vfd->read_cache_start + (int64_t)vfd->read_cache_len;
        if (vfd->logical_offset >= vfd->read_cache_start && vfd->logical_offset < cache_end) {
            int64_t cache_offset = vfd->logical_offset - vfd->read_cache_start;
            uint32_t available = vfd->read_cache_len - (uint32_t)cache_offset;
            size_t to_copy = count < (size_t)available ? count : (size_t)available;

            memcpy(buf, vfd->read_cache + vfd->read_cache_data_offset + cache_offset, to_copy);
            vfd->logical_offset += (int64_t)to_copy;
            return (ssize_t)to_copy;
        }
    }

    fio_cache_block_t *prev = NULL;
    for (fio_cache_block_t *block = vfd->range_cache; block; block = block->next) {
        int64_t cache_end = block->start + (int64_t)block->len;
        if (vfd->logical_offset < block->start || vfd->logical_offset >= cache_end) {
            prev = block;
            continue;
        }

        if (vfd_promote_range_cache_block(vfd, prev, block)) {
            goto retry_active;
        }
        break;
    }

    return 0;
}

static void vfd_update_after_prefetch_response(fio_vfd_t *vfd, uint32_t data_len) {
    vfd->remote_offset = vfd->prefetch_start + (int64_t)data_len;
    if (data_len < vfd->prefetch_request_size) {
        vfd->cached_size = vfd->prefetch_start + (int64_t)data_len;
    }
}

static void vfd_cancel_prefetch(fio_vfd_t *vfd) {
    int slot = vfd->prefetch_slot;
    if (slot < 0) return;

    wait_pending_response(slot);
    int synced_remote_offset = 0;
    if (fio_state.pending[slot].resp_type == FIO_MSG_READ_OK) {
        const uint8_t *data = NULL;
        uint32_t data_len = 0;
        if (decode_read_ok(fio_state.pending[slot].resp_payload,
                           fio_state.pending[slot].resp_len, &(uint16_t){0},
                           &data, &data_len) == 0) {
            vfd_update_after_prefetch_response(vfd, data_len);
            if (vfd_add_range_cache(vfd, vfd->prefetch_start,
                                    fio_state.pending[slot].resp_payload,
                                    2, data_len)) {
                fio_state.pending[slot].resp_payload = NULL;
            }
            synced_remote_offset = 1;
        }
    }
    if (!synced_remote_offset) {
        vfd->remote_offset = -1;
    }

    vfd->prefetch_slot = -1;
    vfd->prefetch_start = 0;
    vfd->prefetch_request_size = 0;
    free_pending(slot);
}

static int vfd_take_prefetch_as_cache(fio_vfd_t *vfd) {
    int slot = vfd->prefetch_slot;
    if (slot < 0 || vfd->logical_offset != vfd->prefetch_start) return 0;

    wait_pending_response(slot);
    vfd->prefetch_slot = -1;

    int result = 0;
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
                           fio_state.pending[slot].resp_len, &(uint16_t){0},
                           &data, &data_len) < 0) {
            errno = EIO;
            result = -1;
        } else {
            vfd_update_after_prefetch_response(vfd, data_len);
            vfd_grow_read_ahead(vfd);
            vfd_retain_or_free_active_read_cache(vfd);
            if (data_len > 0) {
                vfd->read_cache = fio_state.pending[slot].resp_payload;
                vfd->read_cache_data_offset = 2;
                vfd->read_cache_start = vfd->prefetch_start;
                vfd->read_cache_len = data_len;
                fio_state.pending[slot].resp_payload = NULL;
            }
            result = 1;
        }
    } else {
        errno = EIO;
        result = -1;
    }

    vfd->prefetch_start = 0;
    vfd->prefetch_request_size = 0;
    free_pending(slot);
    return result;
}

static void vfd_maybe_start_prefetch(fio_vfd_t *vfd) {
    if (vfd->prefetch_slot >= 0) return;
    if (!vfd->read_cache || vfd->read_cache_len == 0) return;
    if (vfd->dirty) return;

    uint32_t accmode = vfd->wire_flags & 0x0003;
    if (accmode != FIO_O_RDONLY) return;

    int64_t cache_end = vfd->read_cache_start + (int64_t)vfd->read_cache_len;
    if (vfd->remote_offset != cache_end) return;
    if (vfd->logical_offset < vfd->read_cache_start || vfd->logical_offset > cache_end) return;
    if (vfd->cached_size >= 0 && vfd->cached_size < FIO_LARGE_FILE_THRESHOLD) {
        int64_t cache_offset = vfd->logical_offset - vfd->read_cache_start;
        int64_t trigger_offset =
            ((int64_t)vfd->read_cache_len * FIO_SMALL_FILE_PREFETCH_PERCENT + 99) / 100;
        if (cache_offset < trigger_offset) return;
    }

    uint32_t request_size = vfd->read_ahead_bytes;
    if (request_size == 0) return;
    if (!vfd->dirty && vfd->cached_size >= 0 && cache_end >= 0 && cache_end < vfd->cached_size) {
        int64_t remaining = vfd->cached_size - cache_end;
        if (remaining <= 0) return;
        if (remaining < (int64_t)request_size) {
            request_size = (uint32_t)remaining;
        }
    }

    pthread_mutex_lock(&fio_state.send_mutex);
    uint16_t req_id = fio_state.next_req_id++;
    pthread_mutex_unlock(&fio_state.send_mutex);

    uint8_t req_buf[8];
    encode_read_req(req_buf, sizeof(req_buf), req_id, vfd->file_id, request_size);

    int saved_errno = errno;
    int slot = send_async(FIO_MSG_READ, req_buf, 8, req_id);
    if (slot < 0) {
        errno = saved_errno;
        return;
    }

    vfd->prefetch_slot = slot;
    vfd->prefetch_start = cache_end;
    vfd->prefetch_request_size = request_size;
    vfd->remote_offset = cache_end + (int64_t)request_size;
}

static int vfd_remote_seek(fio_vfd_t *vfd, int64_t offset, uint8_t wire_whence,
                           int64_t *new_offset) {
    pthread_mutex_lock(&fio_state.send_mutex);
    uint16_t req_id = fio_state.next_req_id++;
    pthread_mutex_unlock(&fio_state.send_mutex);

    uint8_t req_buf[13];
    encode_seek_req(req_buf, sizeof(req_buf), req_id, vfd->file_id,
                    offset, wire_whence);

    int slot = send_and_wait(FIO_MSG_SEEK, req_buf, 13, req_id);
    if (slot < 0) return -1;

    int result = 0;
    if (fio_state.pending[slot].resp_type == FIO_MSG_IO_ERROR) {
        int32_t wire_err = FIO_EIO;
        decode_io_error(fio_state.pending[slot].resp_payload,
                        fio_state.pending[slot].resp_len, &(uint16_t){0}, &wire_err);
        errno = errno_from_wire(wire_err);
        result = -1;
    } else if (fio_state.pending[slot].resp_type == FIO_MSG_SEEK_OK) {
        int64_t decoded_offset = 0;
        if (decode_seek_ok(fio_state.pending[slot].resp_payload,
                           fio_state.pending[slot].resp_len, &(uint16_t){0},
                           &decoded_offset) < 0) {
            errno = EIO;
            result = -1;
        } else {
            vfd->remote_offset = decoded_offset;
            if (new_offset) *new_offset = decoded_offset;
        }
    } else {
        errno = EIO;
        result = -1;
    }

    free_pending(slot);
    return result;
}

static int vfd_sync_remote_offset(fio_vfd_t *vfd) {
    if (vfd->remote_offset == vfd->logical_offset) return 0;
    int64_t new_offset = 0;
    if (vfd_remote_seek(vfd, vfd->logical_offset, FIO_SEEK_SET, &new_offset) < 0) {
        return -1;
    }
    vfd->logical_offset = new_offset;
    return 0;
}

/* ======================================================================
 * J. Lazy Init
 * ====================================================================== */

/* O_ACCMODE is POSIX; MSVC's fcntl.h omits it. O_RDONLY/O_WRONLY/O_RDWR are
 * 0/1/2 there too, so the mask is the same. */
#ifndef O_ACCMODE
#define O_ACCMODE (O_RDONLY | O_WRONLY | O_RDWR)
#endif

FIO_STATIC int fio_path_has_dotdot(const char *path) {
    const char *p = path;
    while ((p = strstr(p, "/..")) != NULL) {
        if (p[3] == '/' || p[3] == '\0') return 1;
        p += 3;
    }
    return 0;
}

FIO_STATIC void fio_parse_prefix_list(const char *spec,
                                      char (*out)[FIO_MAX_PREFIX_LEN], int *count) {
    *count = 0;
    if (!spec || spec[0] == '\0') return;
#ifdef _WIN32
    const char sep = ';';
#else
    const char sep = ':';
#endif
    const char *p = spec;
    int total = 0;
    while (*p != '\0') {
        const char *end = strchr(p, sep);
        const char *comma = strchr(p, ',');
        if (comma && (!end || comma < end)) end = comma;
        size_t len = end ? (size_t)(end - p) : strlen(p);
        while (len > 1 && p[len - 1] == '/') len--;
        if (len == 0) {
        } else if (p[0] != '/' || len >= FIO_MAX_PREFIX_LEN) {
        } else if (*count < FIO_MAX_PREFIXES) {
            char *slot = out[(*count)++];
            memcpy(slot, p, len);
            slot[len] = '\0';
        }
        total++;
        if (!end) break;
        p = end + 1;
    }
    if (total > FIO_MAX_PREFIXES) {
        fprintf(stderr, "fio: prefix list has %d entries, only first %d kept\n", total, FIO_MAX_PREFIXES);
    }
}

FIO_STATIC void fio_parse_local_prefixes(const char *spec) {
    fio_parse_prefix_list(spec, fio_state.local_prefixes, &fio_state.local_prefix_count);
}

FIO_STATIC void fio_parse_shared_prefixes(const char *spec) {
    fio_parse_prefix_list(spec, fio_state.shared_prefixes, &fio_state.shared_prefix_count);
}

FIO_STATIC int fio_path_matches(const char (*prefixes)[FIO_MAX_PREFIX_LEN],
                                int count, const char *path) {
    if (count == 0) return 0;
    if (!path || path[0] != '/') return 0;
    if (fio_path_has_dotdot(path)) return 0;
    for (int i = 0; i < count; i++) {
        size_t n = strlen(prefixes[i]);
        if (n == 1) return 1;
        if (strncmp(path, prefixes[i], n) != 0) continue;
        if (path[n] == '/' || path[n] == '\0') return 1;
    }
    return 0;
}

FIO_STATIC int fio_path_is_local(const char *path) {
    return fio_path_matches(fio_state.local_prefixes, fio_state.local_prefix_count, path);
}

FIO_STATIC int fio_path_is_shared(const char *path) {
    return fio_path_matches(fio_state.shared_prefixes, fio_state.shared_prefix_count, path);
}

FIO_STATIC int fio_verify_fd_containment(int fd, const char *requested_path) {
    char resolved[4096];
    int ok = 0;
#ifdef __APPLE__
    if (fcntl(fd, F_GETPATH, resolved) == 0) ok = 1;
#else
    {
        char proc_link[64];
        snprintf(proc_link, sizeof(proc_link), "/proc/self/fd/%d", fd);
        ssize_t n = readlink(proc_link, resolved, sizeof(resolved)-1);
        if (n > 0) { resolved[n] = '\0'; ok = 1; }
    }
#endif
    if (!ok) {
        if (!realpath(requested_path, resolved)) return 0;
    }
    for (int list = 0; list < 2; list++) {
        int count = (list == 0) ? fio_state.local_prefix_count : fio_state.shared_prefix_count;
        char (*prefixes)[FIO_MAX_PREFIX_LEN] = (list == 0) ? fio_state.local_prefixes : fio_state.shared_prefixes;
        for (int i = 0; i < count; i++) {
            char pre_real[4096];
            const char *pre = prefixes[i];
            if (!realpath(pre, pre_real)) {
                strncpy(pre_real, pre, sizeof(pre_real)-1);
                pre_real[sizeof(pre_real)-1] = '\0';
            }
            size_t n = strlen(pre_real);
            if (n == 1 && pre_real[0] == '/') return 1;
            if (strncmp(resolved, pre_real, n) != 0) continue;
            if (resolved[n] == '/' || resolved[n] == '\0') return 1;
        }
    }
    return 0;
}

FIO_STATIC int fio_verify_path_shared_containment(const char *path) {
    char resolved[4096];
    char to_check[4096];
    if (realpath(path, resolved)) {
        strncpy(to_check, resolved, sizeof(to_check)-1);
    } else {
        char *dup = strdup(path);
        if (!dup) return 0;
        char *slash = strrchr(dup, '/');
        if (slash) {
            if (slash == dup) strcpy(to_check, "/");
            else {
                *slash = '\0';
                if (!realpath(dup, to_check)) strncpy(to_check, dup, sizeof(to_check)-1);
                size_t len = strlen(to_check);
                size_t base_len = strlen(slash+1);
                if (len + 1 + base_len < sizeof(to_check)) {
                    if (to_check[len-1] != '/') { to_check[len++] = '/'; to_check[len] = '\0'; }
                    strncat(to_check, slash+1, sizeof(to_check)-len-1);
                }
            }
        } else {
            strncpy(to_check, path, sizeof(to_check)-1);
        }
        free(dup);
        to_check[sizeof(to_check)-1] = '\0';
    }
    for (int i = 0; i < fio_state.shared_prefix_count; i++) {
        char pre_real[4096];
        const char *pre = fio_state.shared_prefixes[i];
        if (!realpath(pre, pre_real)) {
            strncpy(pre_real, pre, sizeof(pre_real)-1);
            pre_real[sizeof(pre_real)-1] = '\0';
        }
        size_t n = strlen(pre_real);
        if (n == 1 && pre_real[0] == '/') return 1;
        if (strncmp(to_check, pre_real, n) != 0) continue;
        if (to_check[n] == '/' || to_check[n] == '\0') return 1;
    }
    return 0;
}

static void fio_init(void) {
    memset(&fio_state, 0, sizeof(fio_state));
    fio_state.sock_fd = -1;
    fio_state.next_file_id = 1;
    fio_state.next_req_id = 1;
    fio_state.read_ahead_bytes = FIO_DEFAULT_READAHEAD_BYTES;
    fio_state.range_cache_max_bytes = FIO_RANGE_CACHE_MAX_BYTES;
    pthread_mutex_init(&fio_state.send_mutex, NULL);
    pthread_mutex_init(&fio_state.dispatch_mutex, NULL);
    pthread_cond_init(&fio_state.dispatch_cond, NULL);

    const char *read_ahead_str = getenv("FFOIP_READAHEAD_BYTES");
    if (read_ahead_str && read_ahead_str[0] != '\0') {
        char *end = NULL;
        errno = 0;
        unsigned long long parsed = strtoull(read_ahead_str, &end, 10);
        if (errno == 0 && end && *end == '\0') {
            if (parsed > FIO_MAX_READAHEAD_BYTES) {
                parsed = FIO_MAX_READAHEAD_BYTES;
            }
            fio_state.read_ahead_bytes = (uint32_t)parsed;
            fio_state.read_ahead_explicit = 1;
        } else {
            fprintf(stderr, "fio: invalid FFOIP_READAHEAD_BYTES=%s, using %u\n",
                    read_ahead_str, fio_state.read_ahead_bytes);
        }
    }

    const char *range_cache_str = getenv("FFOIP_RANGE_CACHE_BYTES");
    if (range_cache_str && range_cache_str[0] != '\0') {
        char *end = NULL;
        errno = 0;
        unsigned long long parsed = strtoull(range_cache_str, &end, 10);
        if (errno == 0 && end && *end == '\0') {
            if (parsed > UINT32_MAX) {
                parsed = UINT32_MAX;
            }
            fio_state.range_cache_max_bytes = (uint32_t)parsed;
        } else {
            fprintf(stderr, "fio: invalid FFOIP_RANGE_CACHE_BYTES=%s, using %u\n",
                    range_cache_str, fio_state.range_cache_max_bytes);
        }
    }

    fio_parse_local_prefixes(getenv("FFOIP_SHORT_CIRCUIT_READ"));
    {
        const char *rw = getenv("FFOIP_SHORT_CIRCUIT_READ_WRITE");
        if (!rw || rw[0] == '\0') rw = getenv("FFOIP_SHORT_CIRCUIT_SHARED");
        fio_parse_shared_prefixes(rw);
    }

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

__attribute__((destructor))
static void fio_log_stats(void) {
    if (fio_state.local_prefix_count == 0 && fio_state.shared_prefix_count == 0) return;
    if (fio_state.sc_hits == 0 && fio_state.sc_fallbacks == 0 &&
        fio_state.sc_verify_fail == 0 && fio_state.sc_misses == 0 &&
        fio_state.sc_shared_hits == 0) return;
    fprintf(stderr,
            "fio: short-circuit stats: ro_hits=%llu shared_hits=%llu misses=%llu fallbacks=%llu verify_fail=%llu\n",
            (unsigned long long)fio_state.sc_hits,
            (unsigned long long)fio_state.sc_shared_hits,
            (unsigned long long)fio_state.sc_misses,
            (unsigned long long)fio_state.sc_fallbacks,
            (unsigned long long)fio_state.sc_verify_fail);
}

#ifdef FIO_TESTING
/* ======================================================================
 * J2. Test-only tunnel injection
 *
 * The read-ahead / prefetch / range-cache logic only runs on virtual fds in
 * tunnel mode, which normally needs a live server connection. These hooks let
 * the unit tests drive tunnel mode in-process over a socketpair: one end is a
 * mock server thread, the other is handed here as fio_state.sock_fd.
 * ====================================================================== */

/* Install a pre-connected socket and start the reader thread, forcing tunnel
 * mode. read_ahead_explicit controls whether the small-file read-ahead cap is
 * bypassed (mirrors FFOIP_READAHEAD_BYTES being set). Returns 0 on success. */
int fio_test_set_tunnel(int sock_fd, uint32_t read_ahead_bytes,
                        int read_ahead_explicit, uint32_t range_cache_max_bytes) {
    /* Consume pthread_once so a later fio_* call cannot re-run fio_init and
     * clobber the state we install here. */
    fio_ensure_init();

    memset(&fio_state, 0, sizeof(fio_state));
    fio_state.sock_fd = sock_fd;
    fio_state.next_file_id = 1;
    fio_state.next_req_id = 1;
    fio_state.read_ahead_bytes = read_ahead_bytes;
    fio_state.read_ahead_explicit = read_ahead_explicit;
    fio_state.range_cache_max_bytes = range_cache_max_bytes;
    pthread_mutex_init(&fio_state.send_mutex, NULL);
    pthread_mutex_init(&fio_state.dispatch_mutex, NULL);
    pthread_cond_init(&fio_state.dispatch_cond, NULL);

    if (pthread_create(&fio_state.reader_thread, NULL, reader_thread_func, NULL) != 0) {
        fio_state.sock_fd = -1;
        fio_state.initialized = 1;
        return -1;
    }
    fio_state.initialized = 2;
    return 0;
}

/* Tear down the injected tunnel: drop any caches left on still-open vfds
 * (without touching the socket, since the reader thread may be exiting), close
 * the socket to unblock + join the reader thread, and leave a consistent
 * passthrough state. Safe to call after an early test return. */
void fio_test_teardown(void) {
    for (int i = 0; i < FIO_MAX_FILES; i++) {
        fio_vfd_t *vfd = &fio_state.vfds[i];
        if (vfd->active) {
            vfd->prefetch_slot = -1; /* abandon any in-flight prefetch; no wait */
            vfd_invalidate_read_cache(vfd);
            vfd->active = 0;
        }
    }

    if (fio_state.initialized == 2 && fio_state.sock_fd >= 0) {
        /* shutdown() (not close()) is what reliably unblocks the reader
         * thread's in-progress read() on Linux, where closing an fd does not
         * wake a blocked read on another thread. SHUT_RDWR also delivers EOF
         * to the peer, so the mock server thread exits too. */
        shutdown(fio_state.sock_fd, SHUT_RDWR);
        pthread_join(fio_state.reader_thread, NULL);
        close(fio_state.sock_fd);
        fio_state.sock_fd = -1;
    }
    fio_state.initialized = 1; /* passthrough */
}
#endif /* FIO_TESTING */

/* ======================================================================
 * K. Public API Functions
 * ====================================================================== */

static inline void fio_sc_count(volatile uint64_t *counter) {
    __sync_fetch_and_add(counter, 1);
}

static int fio_remote_verify_and_close(const char *path, int64_t local_size, int *verified) {
    *verified = 0;
    pthread_mutex_lock(&fio_state.send_mutex);
    uint16_t req_id = fio_state.next_req_id++;
    uint16_t file_id = fio_state.next_file_id++;
    pthread_mutex_unlock(&fio_state.send_mutex);
    uint8_t buf[4096];
    int n = encode_open_req(buf, sizeof(buf), req_id, file_id, FIO_O_RDONLY, 0, path);
    if (n < 0) return -1;
    int slot = send_and_wait(FIO_MSG_OPEN, buf, (uint32_t)n, req_id);
    if (slot < 0) return -1;
    int result = -1;
    int64_t remote_size = -1;
    if (fio_state.pending[slot].resp_type == FIO_MSG_OPEN_OK) {
        if (decode_open_ok(fio_state.pending[slot].resp_payload, fio_state.pending[slot].resp_len, &(uint16_t){0}, &remote_size) == 0) {
            result = 0;
            *verified = (remote_size == local_size);
        }
    }
    free_pending(slot);
    if (result == 0) {
        pthread_mutex_lock(&fio_state.send_mutex);
        uint16_t close_req = fio_state.next_req_id++;
        pthread_mutex_unlock(&fio_state.send_mutex);
        uint8_t cbuf[4];
        encode_close_req(cbuf, sizeof(cbuf), close_req, file_id);
        int cslot = send_and_wait(FIO_MSG_CLOSE, cbuf, 4, close_req);
        if (cslot >= 0) free_pending(cslot);
    }
    return result;
}

int fio_open(const char *path, int flags, mode_t mode) {
    fio_ensure_init();
    if (fio_state.initialized == 1) {
        return open(path, flags, mode);
    }

    /* RW shared: any open under shared prefix tries local with containment */
    if (fio_path_is_shared(path)) {
        int local_fd = open(path, flags, mode);
        if (local_fd >= 0) {
            if (!fio_verify_fd_containment(local_fd, path)) {
                close(local_fd);
                fio_sc_count(&fio_state.sc_fallbacks);
            } else {
                fio_sc_count(&fio_state.sc_shared_hits);
                return local_fd;
            }
        } else {
            fio_sc_count(&fio_state.sc_misses);
        }
    }

    /* RO: only read-only, verified via size check */
    int is_ro = ((flags & O_ACCMODE) == O_RDONLY) && !(flags & (O_CREAT | O_TRUNC));
    if (is_ro && fio_path_is_local(path)) {
        int local_fd = open(path, flags, mode);
        if (local_fd >= 0) {
            struct stat st;
            if (fstat(local_fd, &st) != 0) {
                close(local_fd);
                fio_sc_count(&fio_state.sc_fallbacks);
            } else if (!fio_verify_fd_containment(local_fd, path)) {
                close(local_fd);
                fio_sc_count(&fio_state.sc_fallbacks);
            } else {
                int verified = 0;
                int vr = fio_remote_verify_and_close(path, (int64_t)st.st_size, &verified);
                if (vr == 0 && verified) {
                    fio_sc_count(&fio_state.sc_hits);
                    return local_fd;
                }
                close(local_fd);
                if (vr == 0 && !verified) {
                    fio_sc_count(&fio_state.sc_verify_fail);
                } else {
                    fio_sc_count(&fio_state.sc_fallbacks);
                }
            }
        } else {
            fio_sc_count(&fio_state.sc_misses);
        }
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
            if (result < 0) {
                errno = ENOMEM;
                result = -1;
            } else {
                fio_vfd_t *vfd = vfd_get(result);
                if (vfd) vfd->wire_flags = wire_flags;
            }
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

    if (count == 0) return 0;

    int at_cache_end = vfd->read_cache &&
        vfd->logical_offset == vfd->read_cache_start + (int64_t)vfd->read_cache_len;

    ssize_t cached = vfd_copy_from_cache(vfd, buf, count);
    if (cached > 0) {
        vfd_maybe_start_prefetch(vfd);
        return cached;
    }

    if (vfd->prefetch_slot >= 0) {
        int prefetch_result = vfd_take_prefetch_as_cache(vfd);
        if (prefetch_result < 0) return -1;
        if (prefetch_result > 0) {
            cached = vfd_copy_from_cache(vfd, buf, count);
            if (cached > 0) {
                vfd_maybe_start_prefetch(vfd);
                return cached;
            }
            return 0;
        }
        vfd_cancel_prefetch(vfd);
    }

    if (vfd_sync_remote_offset(vfd) < 0) return -1;
    if (at_cache_end) {
        vfd_grow_read_ahead(vfd);
    }

    pthread_mutex_lock(&fio_state.send_mutex);
    uint16_t req_id = fio_state.next_req_id++;
    pthread_mutex_unlock(&fio_state.send_mutex);

    uint32_t request_size = (uint32_t)(count > 0xFFFFFFFF ? 0xFFFFFFFF : count);
    uint32_t accmode = vfd->wire_flags & 0x0003;
    if (accmode == FIO_O_RDONLY && vfd->read_ahead_bytes > request_size) {
        request_size = vfd->read_ahead_bytes;
    }
    if (!vfd->dirty && vfd->cached_size >= 0 && vfd->logical_offset >= 0 &&
        vfd->logical_offset < vfd->cached_size) {
        int64_t remaining = vfd->cached_size - vfd->logical_offset;
        if (remaining > 0 && remaining < (int64_t)request_size) {
            request_size = (uint32_t)remaining;
        }
    }

    uint8_t req_buf[8];
    encode_read_req(req_buf, sizeof(req_buf), req_id, vfd->file_id, request_size);

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
            int64_t response_start = vfd->logical_offset;
            vfd->remote_offset += (int64_t)data_len;
            if ((uint32_t)data_len < request_size) {
                vfd->cached_size = response_start + (int64_t)data_len;
            }

            if ((size_t)data_len > count) {
                vfd_retain_or_free_active_read_cache(vfd);
                vfd->read_cache = fio_state.pending[slot].resp_payload;
                vfd->read_cache_data_offset = 2;
                vfd->read_cache_start = response_start;
                vfd->read_cache_len = data_len;
                fio_state.pending[slot].resp_payload = NULL;
                result = vfd_copy_from_cache(vfd, buf, count);
                if (result > 0) {
                    vfd_maybe_start_prefetch(vfd);
                }
            } else {
                memcpy(buf, data, data_len);
                vfd->logical_offset += (int64_t)data_len;
                vfd_retain_or_free_active_read_cache(vfd);
                result = (ssize_t)data_len;
            }
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

    vfd_cancel_prefetch(vfd);
    vfd_invalidate_read_cache(vfd);
    vfd_reset_read_ahead(vfd);
    if (vfd_sync_remote_offset(vfd) < 0) return -1;

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
            vfd->logical_offset += (int64_t)written;
            vfd->remote_offset += (int64_t)written;
            if (vfd->logical_offset > vfd->cached_size) {
                vfd->cached_size = vfd->logical_offset;
            }
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

    int64_t target = 0;
    switch (whence) {
    case SEEK_SET:
        target = (int64_t)offset;
        break;
    case SEEK_CUR:
        target = vfd->logical_offset + (int64_t)offset;
        break;
    case SEEK_END:
        if (vfd->dirty) {
            vfd_cancel_prefetch(vfd);
            int64_t new_offset = 0;
            if (vfd_remote_seek(vfd, (int64_t)offset, FIO_SEEK_END, &new_offset) < 0) {
                return -1;
            }
            vfd->logical_offset = new_offset;
            vfd_invalidate_read_cache(vfd);
            vfd_reset_read_ahead(vfd);
            return (off_t)new_offset;
        }
        target = vfd->cached_size + (int64_t)offset;
        break;
    default:
        errno = EINVAL;
        return -1;
    }

    if (target < 0) {
        errno = EINVAL;
        return -1;
    }

    if (vfd_cache_contains(vfd, target) ||
        (vfd->read_cache && target == vfd->read_cache_start + (int64_t)vfd->read_cache_len)) {
        vfd->logical_offset = target;
        return (off_t)target;
    }

    vfd_cancel_prefetch(vfd);
    int64_t new_offset = 0;
    if (vfd_remote_seek(vfd, target, FIO_SEEK_SET, &new_offset) < 0) {
        return -1;
    }
    vfd->logical_offset = new_offset;
    vfd_retain_or_free_active_read_cache(vfd);
    vfd_reset_read_ahead(vfd);
    return (off_t)new_offset;
}

int fio_close(int fd) {
    fio_ensure_init();

    if (fio_state.initialized == 1 || is_real_fd(fd)) {
        return close(fd);
    }

    fio_vfd_t *vfd = vfd_get(fd);
    if (!vfd) { errno = EBADF; return -1; }

    vfd_cancel_prefetch(vfd);
    vfd_invalidate_read_cache(vfd);
    vfd_reset_read_ahead(vfd);

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

    vfd_cancel_prefetch(vfd);
    vfd_invalidate_read_cache(vfd);
    vfd_reset_read_ahead(vfd);

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
        vfd->cached_size = (int64_t)length;
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
    if (fio_path_is_shared(path) && fio_verify_path_shared_containment(path)) {
        int rc = unlink(path);
        if (rc == 0) {
            fio_sc_count(&fio_state.sc_shared_hits);
            return 0;
        }
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
    if (fio_path_is_shared(oldpath) && fio_path_is_shared(newpath) &&
        fio_verify_path_shared_containment(oldpath) &&
        fio_verify_path_shared_containment(newpath)) {
        int rc = rename(oldpath, newpath);
        if (rc == 0) {
            fio_sc_count(&fio_state.sc_shared_hits);
            return 0;
        }
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
    if (fio_path_is_shared(path) && fio_verify_path_shared_containment(path)) {
        int rc;
#ifdef _WIN32
        (void)mode;
        rc = _mkdir(path);
#else
        rc = mkdir(path, mode);
#endif
        if (rc == 0) {
            fio_sc_count(&fio_state.sc_shared_hits);
            return 0;
        }
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
