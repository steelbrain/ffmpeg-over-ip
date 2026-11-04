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
#include <netinet/tcp.h>
#include <arpa/inet.h>
#include <dirent.h>
#endif

#ifdef HAVE_AV_CONFIG_H
/* Built inside libavformat. Passthrough mode uses the same wrappers as the
 * unpatched file protocol: avpriv_open (O_CLOEXEC, UTF-8 paths on Windows)
 * and os_support.h, which on Windows maps stat/fstat/access/rename/mkdir/
 * unlink to UTF-8-aware versions and makes `struct stat` the same
 * win32_stat that file.c passes in. */
#include "config.h"
#include "libavutil/file_open.h"
#include "os_support.h"
#define fio_passthrough_open avpriv_open
#else
#define fio_passthrough_open open
#endif

/* After os_support.h, as in file.c: on Windows os_support.h renames
 * `struct stat` to `struct win32_stat`, and fio_fstat/fio_stat must be
 * declared with the same struct that file.c and img2dec.c pass in. */
#include "fio.h"

#ifndef R_OK
#define R_OK 4
#endif
#ifndef W_OK
#define W_OK 2
#endif
#ifndef X_OK
#define X_OK 1
#endif

/* When FIO_TESTING is defined, encoders, decoders and helpers marked
 * FIO_STATIC are externally visible for the unit tests. */
#ifdef FIO_TESTING
#define FIO_STATIC
#else
#define FIO_STATIC static
#endif

#if defined(_WIN32) && !defined(HAVE_AV_CONFIG_H)
/* Outside libavformat (the unit tests and tools), `struct stat` is mingw's
 * own, whose st_size is 32 bits unless _FILE_OFFSET_BITS=64. Inside
 * libavformat it is os_support.h's win32_stat, always 64-bit. */
_Static_assert(sizeof(((struct stat *)0)->st_size) == 8,
               "build fio.c with -D_FILE_OFFSET_BITS=64 on Windows");
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
#define FIO_MSG_STAT        0x2A
#define FIO_MSG_LISTDIR     0x2B

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
#define FIO_MSG_STAT_OK        0x4A
#define FIO_MSG_LISTDIR_OK     0x4B
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
#define FIO_EBADF     9
#define FIO_EACCES   13
#define FIO_EBUSY    16
#define FIO_EEXIST   17
#define FIO_EXDEV    18
#define FIO_ENOTDIR  20
#define FIO_EISDIR   21
#define FIO_EINVAL   22
#define FIO_EFBIG    27
#define FIO_ENOSPC   28
#define FIO_ESPIPE   29
#define FIO_EROFS    30
#define FIO_EPIPE    32
#define FIO_ERANGE   34
#define FIO_ENAMETOOLONG 36
#define FIO_ENOTEMPTY    39
#define FIO_ELOOP        40
#define FIO_EOVERFLOW    75
#define FIO_EDQUOT      122

/* Canonical file mode bits on the wire: POSIX st_mode values as on Linux. */
#define FIO_S_IFMT    0170000
#define FIO_S_IFSOCK  0140000
#define FIO_S_IFLNK   0120000
#define FIO_S_IFREG   0100000
#define FIO_S_IFBLK   0060000
#define FIO_S_IFDIR   0040000
#define FIO_S_IFCHR   0020000
#define FIO_S_IFIFO   0010000

/* Access bits in a StatOk reply */
#define FIO_ACCESS_R  0x01
#define FIO_ACCESS_W  0x02
#define FIO_ACCESS_X  0x04

/* Largest READ or WRITE fio sends in one request. A bigger read() or
 * write() returns a short count, which POSIX allows and ffmpeg's URL layer
 * retries. Without the cap a large write or read could exceed the 100 MiB
 * message limit, and the server would drop the whole connection. */
#define FIO_MAX_IO_BYTES (16U * 1024U * 1024U)

/* Largest response the reader thread accepts. The Go server rejects bigger
 * messages too, so anything larger means the stream is corrupt. */
#define FIO_MAX_RESPONSE_BYTES (100U * 1024U * 1024U)

/* Largest file size or offset accepted from the client. Offsets grow by at
 * most one response at a time, so anything below this can't overflow
 * int64_t; a bigger value can only come from a broken client. */
#define FIO_MAX_REMOTE_OFFSET (INT64_MAX - (int64_t)FIO_MAX_RESPONSE_BYTES)

/* First bytes on the loopback connection: magic + the FFOIP_TOKEN value
 * (see fio_init_tunnel). Must match internal/process. */
#define FIO_HELLO_MAGIC  "FIO1"
#define FIO_TOKEN_LEN    64
#define FIO_HELLO_LEN    (4 + FIO_TOKEN_LEN)

/* Virtual FD table. Virtual fds are FIO_VFD_BASE..FIO_VFD_BASE+FIO_MAX_FILES-1,
 * far above any descriptor the kernel or the Windows CRT hands out, so the
 * real fds ffmpeg uses alongside them (pipe:, fd:, stdio) never collide. */
#define FIO_VFD_BASE     0x40000000
#define FIO_MAX_FILES    256

/* Pending request slots. Each open file parks at most one prefetch, and
 * prefetches leave FIO_SYNC_RESERVED_SLOTS free for synchronous requests
 * (see send_async). */
#define FIO_SYNC_RESERVED_SLOTS 64
#define FIO_MAX_PENDING  (FIO_MAX_FILES + FIO_SYNC_RESERVED_SLOTS)

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
    uint32_t wire_mode;    /* st_mode bits from OPEN_OK (wire encoding) */
    int      regular;      /* a regular file: pipes and devices get no
                              prefetch, since a read can block until the
                              writer produces data */
    int64_t  cached_size;
    int64_t  logical_offset;
    int64_t  remote_offset;
    int64_t  read_cache_start;
    uint8_t *read_cache;
    uint32_t read_cache_data_offset;
    uint32_t read_cache_len;
    fio_cache_block_t *range_cache;
    uint64_t range_cache_bytes;
    uint32_t read_ahead_bytes;
    int      prefetch_slot;
    int64_t  prefetch_start;
    int      size_grew;    /* a read returned data past cached_size: the file
                              is growing, so cached_size is only a lower bound */
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
    uint16_t          next_file_id; /* protected by vfd_mutex */
    uint16_t          next_req_id;  /* protected by dispatch_mutex */
    int               pending_used; /* used pending slots; dispatch_mutex */
    pthread_mutex_t   send_mutex;
    pthread_mutex_t   dispatch_mutex;
    pthread_cond_t    dispatch_cond;
    pthread_mutex_t   vfd_mutex;    /* vfd slot allocation, file ids and
                                       range_cache_total */
    uint64_t          range_cache_total; /* range cache bytes held by all
                                            files; vfd_mutex */
    int               tunnel_dead;  /* reader thread has exited; protected
                                       by dispatch_mutex */
    uint32_t          read_ahead_bytes;
    int               read_ahead_explicit;
    uint32_t          range_cache_max_bytes;
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
    case EBUSY:   return FIO_EBUSY;
    case EEXIST:  return FIO_EEXIST;
    case EXDEV:   return FIO_EXDEV;
    case ENOTDIR: return FIO_ENOTDIR;
    case EISDIR:  return FIO_EISDIR;
    case EINVAL:  return FIO_EINVAL;
    case ENOSPC:  return FIO_ENOSPC;
#ifdef EROFS
    case EROFS:   return FIO_EROFS;
#endif
    case ERANGE:  return FIO_ERANGE;
    case ENAMETOOLONG: return FIO_ENAMETOOLONG;
    case ENOTEMPTY:    return FIO_ENOTEMPTY;
#ifdef ELOOP
    case ELOOP:        return FIO_ELOOP;
#endif
    case EBADF:   return FIO_EBADF;
    case EFBIG:   return FIO_EFBIG;
    case ESPIPE:  return FIO_ESPIPE;
    case EPIPE:   return FIO_EPIPE;
#ifdef EOVERFLOW
    case EOVERFLOW: return FIO_EOVERFLOW;
#endif
#ifdef EDQUOT
    case EDQUOT:  return FIO_EDQUOT;
#endif
    default:      return FIO_EIO;
    }
}

static int errno_from_wire(int32_t wire_err) {
    switch (wire_err) {
    case FIO_EPERM:   return EPERM;
    case FIO_ENOENT:  return ENOENT;
    case FIO_EIO:     return EIO;
    case FIO_EACCES:  return EACCES;
    case FIO_EBUSY:   return EBUSY;
    case FIO_EEXIST:  return EEXIST;
    case FIO_EXDEV:   return EXDEV;
    case FIO_ENOTDIR: return ENOTDIR;
    case FIO_EISDIR:  return EISDIR;
    case FIO_EINVAL:  return EINVAL;
    case FIO_ENOSPC:  return ENOSPC;
#ifdef EROFS
    case FIO_EROFS:   return EROFS;
#endif
    case FIO_ERANGE:  return ERANGE;
    case FIO_ENAMETOOLONG: return ENAMETOOLONG;
    case FIO_ENOTEMPTY:    return ENOTEMPTY;
#ifdef ELOOP
    case FIO_ELOOP:        return ELOOP;
#endif
    case FIO_EBADF:   return EBADF;
    case FIO_EFBIG:   return EFBIG;
    case FIO_ESPIPE:  return ESPIPE;
    case FIO_EPIPE:   return EPIPE;
#ifdef EOVERFLOW
    case FIO_EOVERFLOW: return EOVERFLOW;
#endif
#ifdef EDQUOT
    case FIO_EDQUOT:  return EDQUOT;
#else
    case FIO_EDQUOT:  return ENOSPC; /* no quota errno here (Windows) */
#endif
    default:          return EIO;
    }
}

/* Go's os.FileMode type bits. Clients before the canonical-mode change sent
 * these in FstatOk instead of POSIX st_mode bits. */
#define FIO_GO_MODE_DIR        (1U << 31)
#define FIO_GO_MODE_SYMLINK    (1U << 27)
#define FIO_GO_MODE_DEVICE     (1U << 26)
#define FIO_GO_MODE_NAMEDPIPE  (1U << 25)
#define FIO_GO_MODE_SOCKET     (1U << 24)
#define FIO_GO_MODE_CHARDEVICE (1U << 21)

/* Convert a wire mode to this platform's st_mode. The wire carries POSIX
 * st_mode bits; a value with bits above 0xFFFF is an older client's Go
 * os.FileMode and is translated. A mode with no type bits is taken to be a
 * regular file, which is what an open file descriptor normally is. */
FIO_STATIC unsigned int mode_from_wire(uint32_t wire) {
    uint32_t type;
    if (wire & 0xFFFF0000U) {
        if (wire & FIO_GO_MODE_DIR) type = FIO_S_IFDIR;
        else if (wire & FIO_GO_MODE_SYMLINK) type = FIO_S_IFLNK;
        else if (wire & FIO_GO_MODE_NAMEDPIPE) type = FIO_S_IFIFO;
        else if (wire & FIO_GO_MODE_SOCKET) type = FIO_S_IFSOCK;
        else if (wire & FIO_GO_MODE_DEVICE)
            type = (wire & FIO_GO_MODE_CHARDEVICE) ? FIO_S_IFCHR : FIO_S_IFBLK;
        else if (wire & FIO_GO_MODE_CHARDEVICE) type = FIO_S_IFCHR;
        else type = FIO_S_IFREG;
    } else {
        type = wire & FIO_S_IFMT;
        if (type == 0) type = FIO_S_IFREG;
    }

    unsigned int mode = wire & 0777;
    switch (type) {
    case FIO_S_IFDIR:  mode |= S_IFDIR; break;
    case FIO_S_IFCHR:  mode |= S_IFCHR; break;
#ifdef S_IFIFO
    case FIO_S_IFIFO:  mode |= S_IFIFO; break;
#endif
#ifdef S_IFBLK
    case FIO_S_IFBLK:  mode |= S_IFBLK; break;
#endif
#ifdef S_IFLNK
    case FIO_S_IFLNK:  mode |= S_IFLNK; break;
#endif
#ifdef S_IFSOCK
    case FIO_S_IFSOCK: mode |= S_IFSOCK; break;
#endif
    default:           mode |= S_IFREG; break;
    }
    return mode;
}

/* ======================================================================
 * E. Encoding helpers for each request type
 * ====================================================================== */

/* All encode functions return the number of bytes written, or -1 on error. */

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

/* OpenOkResponse: req_id(2) + file_size(8) + mode(4) = 14. mode is the
 * opened file's POSIX st_mode bits, as in FstatOk. */
FIO_STATIC int encode_open_ok(uint8_t *buf, uint32_t cap,
                              uint16_t req_id, int64_t file_size, uint32_t mode) {
    if (cap < 14) return -1;
    put_u16(buf, req_id);
    put_u64(buf + 2, (uint64_t)file_size);
    put_u32(buf + 10, mode);
    return 14;
}

FIO_STATIC int decode_open_ok(const uint8_t *buf, uint32_t len,
                              uint16_t *req_id, int64_t *file_size, uint32_t *mode) {
    if (len < 14) return -1;
    *req_id    = get_u16(buf);
    *file_size = (int64_t)get_u64(buf + 2);
    *mode      = get_u32(buf + 10);
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

/* StatRequest and ListDirRequest: req_id(2) + path(variable), the same
 * layout as UnlinkRequest. */
FIO_STATIC int encode_path_req(uint8_t *buf, uint32_t cap,
                               uint16_t req_id, const char *path) {
    return encode_unlink_req(buf, cap, req_id, path);
}

/* StatOkResponse: req_id(2) + size(8) + mode(4) + mtime(8) + access(1) = 23.
 * mode is POSIX st_mode bits, mtime is Unix seconds, access is FIO_ACCESS_*
 * as seen by the client process. */
FIO_STATIC int encode_stat_ok(uint8_t *buf, uint32_t cap, uint16_t req_id,
                              int64_t size, uint32_t mode, int64_t mtime,
                              uint8_t access_bits) {
    if (cap < 23) return -1;
    put_u16(buf, req_id);
    put_u64(buf + 2, (uint64_t)size);
    put_u32(buf + 10, mode);
    put_u64(buf + 14, (uint64_t)mtime);
    buf[22] = access_bits;
    return 23;
}

FIO_STATIC int decode_stat_ok(const uint8_t *buf, uint32_t len, uint16_t *req_id,
                              int64_t *size, uint32_t *mode, int64_t *mtime,
                              uint8_t *access_bits) {
    if (len < 23) return -1;
    *req_id = get_u16(buf);
    *size   = (int64_t)get_u64(buf + 2);
    *mode   = get_u32(buf + 10);
    *mtime  = (int64_t)get_u64(buf + 14);
    *access_bits = buf[22];
    return 0;
}

/* ListDirOkResponse: req_id(2) + count(4) + count entries of
 * name_len(2) + name + mode(4) + size(8) + mtime(8). Entries describe the
 * directory entry itself (lstat), never "." or "..". */
FIO_STATIC int encode_listdir_ok_header(uint8_t *buf, uint32_t cap,
                                        uint16_t req_id, uint32_t count) {
    if (cap < 6) return -1;
    put_u16(buf, req_id);
    put_u32(buf + 2, count);
    return 6;
}

FIO_STATIC int encode_listdir_entry(uint8_t *buf, uint32_t cap, const char *name,
                                    uint32_t mode, int64_t size, int64_t mtime) {
    size_t raw_len = strlen(name);
    if (raw_len > 0xFFFF) return -1;
    uint32_t need = 2 + (uint32_t)raw_len + 20;
    if (cap < need) return -1;
    put_u16(buf, (uint16_t)raw_len);
    memcpy(buf + 2, name, raw_len);
    put_u32(buf + 2 + raw_len, mode);
    put_u64(buf + 6 + raw_len, (uint64_t)size);
    put_u64(buf + 14 + raw_len, (uint64_t)mtime);
    return (int)need;
}

FIO_STATIC int decode_listdir_header(const uint8_t *buf, uint32_t len,
                                     uint16_t *req_id, uint32_t *count) {
    if (len < 6) return -1;
    *req_id = get_u16(buf);
    *count  = get_u32(buf + 2);
    return 6;
}

/* Decode the entry starting at *pos and advance *pos past it. The name is not
 * NUL-terminated. Returns 0, or -1 if the entry runs past len. */
FIO_STATIC int decode_listdir_entry(const uint8_t *buf, uint32_t len, uint32_t *pos,
                                    const uint8_t **name, uint16_t *name_len,
                                    uint32_t *mode, int64_t *size, int64_t *mtime) {
    uint32_t p = *pos;
    if (p > len || len - p < 2) return -1;
    uint16_t nl = get_u16(buf + p);
    if (len - p - 2 < (uint32_t)nl + 20) return -1;
    *name     = buf + p + 2;
    *name_len = nl;
    *mode     = get_u32(buf + p + 2 + nl);
    *size     = (int64_t)get_u64(buf + p + 6 + nl);
    *mtime    = (int64_t)get_u64(buf + p + 14 + nl);
    *pos      = p + 2 + nl + 20;
    return 0;
}

/* ======================================================================
 * F. Socket I/O helpers
 * ====================================================================== */

/* Write exactly n bytes to fd. Returns 0 on success, -1 on error. A signal
 * that interrupts the call is retried. */
static int write_all(int fd, const uint8_t *buf, size_t n) {
    while (n > 0) {
#ifdef _WIN32
        int w = send(fd, (const char *)buf, (int)n, 0);
#elif defined(MSG_NOSIGNAL)
        /* A server that went away must show up as EPIPE, not kill ffmpeg
         * with SIGPIPE. macOS has no MSG_NOSIGNAL; fio_init sets
         * SO_NOSIGPIPE on the socket instead. */
        ssize_t w = send(fd, buf, n, MSG_NOSIGNAL);
#else
        ssize_t w = write(fd, buf, n);
#endif
#ifdef _WIN32
        /* Winsock reports errors through WSAGetLastError, not errno. */
        if (w == SOCKET_ERROR && WSAGetLastError() == WSAEINTR) continue;
#else
        if (w < 0 && errno == EINTR) continue;
#endif
        if (w <= 0) return -1;
        buf += w;
        n -= (size_t)w;
    }
    return 0;
}

/* Read exactly n bytes from fd. Returns 0 on success, -1 on error or EOF. A
 * signal that interrupts the call is retried. */
static int read_all(int fd, uint8_t *buf, size_t n) {
    while (n > 0) {
#ifdef _WIN32
        int r = recv(fd, (char *)buf, (int)n, 0);
#else
        ssize_t r = read(fd, buf, n);
#endif
#ifdef _WIN32
        if (r == SOCKET_ERROR && WSAGetLastError() == WSAEINTR) continue;
#else
        if (r < 0 && errno == EINTR) continue;
#endif
        if (r <= 0) return -1;
        buf += r;
        n -= (size_t)r;
    }
    return 0;
}

/* Requests up to this size go out in one send(). Every request except a
 * WRITE's data is far smaller. */
#define FIO_SEND_COALESCE 4096

/* Send an envelope on the socket. Caller must hold send_mutex. */
static int send_envelope(uint8_t type, const uint8_t *payload, uint32_t payload_len) {
    uint8_t buf[5 + FIO_SEND_COALESCE];
    buf[0] = type;
    put_u32(buf + 1, payload_len);
    if (payload_len <= FIO_SEND_COALESCE) {
        if (payload_len > 0) memcpy(buf + 5, payload, payload_len);
        return write_all(fio_state.sock_fd, buf, 5 + (size_t)payload_len);
    }
    if (write_all(fio_state.sock_fd, buf, 5) < 0) return -1;
    return write_all(fio_state.sock_fd, payload, payload_len);
}

/* ======================================================================
 * G. Response Dispatching
 * ====================================================================== */

/* Extract req_id from any response payload (always first 2 bytes) */
static uint16_t extract_req_id(const uint8_t *payload, uint32_t len) {
    if (len < 2) return 0;
    return get_u16(payload);
}

/* Called once the reader thread can no longer receive responses (the server
 * closed the connection or the stream is corrupt). Every request still
 * waiting gets an IO_ERROR so its caller returns EIO instead of blocking
 * forever, and later requests fail immediately (see send_async).
 *
 * A slot is unanswered while resp_type is 0. Only resp_type is checked here
 * and in the reader thread: once a reply is in, the slot's owner takes
 * resp_payload (sets it to NULL) without holding dispatch_mutex. */
static void mark_tunnel_dead(void) {
    pthread_mutex_lock(&fio_state.dispatch_mutex);
    fio_state.tunnel_dead = 1;
    for (int i = 0; i < FIO_MAX_PENDING; i++) {
        if (fio_state.pending[i].used &&
            fio_state.pending[i].resp_type == 0) {
            fio_state.pending[i].resp_type = FIO_MSG_IO_ERROR;
            fio_state.pending[i].resp_len  = 0;
        }
    }
    pthread_cond_broadcast(&fio_state.dispatch_cond);
    pthread_mutex_unlock(&fio_state.dispatch_mutex);
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
        if (plen > FIO_MAX_RESPONSE_BYTES) break;
        /* resp_type 0 marks an unanswered slot, so no reply can have it. */
        if (type == 0) break;

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
            if (fio_state.pending[i].used && fio_state.pending[i].req_id == req_id &&
                fio_state.pending[i].resp_type == 0) {
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
    mark_tunnel_dead();
    return NULL;
}

/* ======================================================================
 * H. send_and_wait Helper
 * ====================================================================== */

/* Free a pending slot after processing, waking any request waiting for one. */
static void free_pending(int slot) {
    pthread_mutex_lock(&fio_state.dispatch_mutex);
    free(fio_state.pending[slot].resp_payload);
    fio_state.pending[slot].resp_payload = NULL;
    fio_state.pending[slot].used = 0;
    fio_state.pending_used--;
    pthread_cond_broadcast(&fio_state.dispatch_cond);
    pthread_mutex_unlock(&fio_state.dispatch_mutex);
}

/* Pick a request id no in-flight request uses. A parked prefetch can hold
 * its slot for a long time while the 16-bit counter wraps, and the reader
 * thread matches responses by id, so reusing a live id would hand one
 * request's reply to another. 0 is never used: extract_req_id returns it for
 * malformed replies. Caller must hold dispatch_mutex. */
static uint16_t next_free_req_id(void) {
    for (;;) {
        uint16_t id = fio_state.next_req_id++;
        if (id == 0) continue;
        int taken = 0;
        for (int i = 0; i < FIO_MAX_PENDING; i++) {
            if (fio_state.pending[i].used && fio_state.pending[i].req_id == id) {
                taken = 1;
                break;
            }
        }
        if (!taken) return id; /* at most FIO_MAX_PENDING ids are taken */
    }
}

/* Allocate a pending slot and give it a fresh request id. Returns the slot
 * index or -1 if every slot is used. Caller must hold dispatch_mutex. */
static int alloc_pending(void) {
    for (int i = 0; i < FIO_MAX_PENDING; i++) {
        if (!fio_state.pending[i].used) {
            fio_state.pending[i].req_id       = next_free_req_id();
            fio_state.pending[i].used         = 1;
            fio_state.pending[i].resp_type    = 0;
            fio_state.pending[i].resp_payload = NULL;
            fio_state.pending[i].resp_len     = 0;
            fio_state.pending_used++;
            return i;
        }
    }
    return -1;
}

/* Send a request without waiting for the reply. The payload must start with
 * a 2-byte req_id field; send_async fills it in. Returns the pending slot
 * index, or -1 with errno set.
 *
 * A synchronous request (is_prefetch == 0) waits for a free slot when all
 * are taken. A prefetch never waits: it fails with EAGAIN unless more than
 * FIO_SYNC_RESERVED_SLOTS slots are free, so parked prefetches (at most one
 * per open file) can never starve synchronous requests. */
static int send_async(uint8_t msg_type, uint8_t *payload, uint32_t payload_len,
                      int is_prefetch) {
    pthread_mutex_lock(&fio_state.dispatch_mutex);
    int slot = -1;
    for (;;) {
        if (fio_state.tunnel_dead) {
            pthread_mutex_unlock(&fio_state.dispatch_mutex);
            errno = EIO;
            return -1;
        }
        int free_slots = FIO_MAX_PENDING - fio_state.pending_used;
        if (is_prefetch) {
            if (free_slots <= FIO_SYNC_RESERVED_SLOTS) {
                pthread_mutex_unlock(&fio_state.dispatch_mutex);
                errno = EAGAIN;
                return -1;
            }
        } else if (free_slots == 0) {
            pthread_cond_wait(&fio_state.dispatch_cond, &fio_state.dispatch_mutex);
            continue;
        }
        slot = alloc_pending();
        break;
    }
    put_u16(payload, fio_state.pending[slot].req_id);
    pthread_mutex_unlock(&fio_state.dispatch_mutex);

    pthread_mutex_lock(&fio_state.send_mutex);
    int rc = send_envelope(msg_type, payload, payload_len);
    pthread_mutex_unlock(&fio_state.send_mutex);
    if (rc < 0) {
        /* The request may have been cut off mid-message, so nothing sent
         * after it would be framed correctly. Treat the tunnel as dead. */
        mark_tunnel_dead();
        free_pending(slot);
        errno = EIO;
        return -1;
    }

    return slot;
}

/* Block until the slot has a response. If the tunnel dies first, the slot is
 * completed with an IO_ERROR (no payload), which callers report as EIO. */
static void wait_pending_response(int slot) {
    pthread_mutex_lock(&fio_state.dispatch_mutex);
    while (fio_state.pending[slot].resp_type == 0 && !fio_state.tunnel_dead) {
        pthread_cond_wait(&fio_state.dispatch_cond, &fio_state.dispatch_mutex);
    }
    if (fio_state.pending[slot].resp_type == 0) {
        fio_state.pending[slot].resp_type = FIO_MSG_IO_ERROR;
        fio_state.pending[slot].resp_len  = 0;
    }
    pthread_mutex_unlock(&fio_state.dispatch_mutex);
}

static int send_and_wait(uint8_t msg_type, uint8_t *payload, uint32_t payload_len) {
    int slot = send_async(msg_type, payload, payload_len, 0);
    if (slot < 0) return -1;
    wait_pending_response(slot);
    return slot;
}

/* ======================================================================
 * I. Virtual FD Table
 * ====================================================================== */

static uint32_t vfd_initial_read_ahead(void);

static int file_id_in_use(uint16_t file_id) {
    for (int i = 0; i < FIO_MAX_FILES; i++) {
        if (fio_state.vfds[i].active && fio_state.vfds[i].file_id == file_id) return 1;
    }
    return 0;
}

/* Claim a free vfd slot and a file id that no open (or opening) file uses,
 * before the OPEN request is sent. Both happen under vfd_mutex because ffmpeg
 * opens files from several threads at once, and the 16-bit file id wraps
 * after 65535 opens (long HLS sessions get there) while files opened early,
 * like the input, are still open under their original ids. The server
 * rejects an OPEN that reuses a live id. Returns the virtual fd, or -1 with
 * errno set. */
static int vfd_reserve(uint16_t *file_id_out) {
    pthread_mutex_lock(&fio_state.vfd_mutex);

    int idx = -1;
    for (int i = 0; i < FIO_MAX_FILES; i++) {
        if (!fio_state.vfds[i].active) { idx = i; break; }
    }
    if (idx < 0) {
        pthread_mutex_unlock(&fio_state.vfd_mutex);
        errno = EMFILE;
        return -1;
    }

    /* At most FIO_MAX_FILES - 1 ids are taken, so this finds one quickly. */
    uint16_t file_id;
    do {
        file_id = fio_state.next_file_id++;
    } while (file_id == 0 || file_id_in_use(file_id));

    fio_vfd_t *vfd = &fio_state.vfds[idx];
    memset(vfd, 0, sizeof(*vfd));
    vfd->active = 1;
    vfd->file_id = file_id;
    vfd->prefetch_slot = -1;
    vfd->read_ahead_bytes = vfd_initial_read_ahead();

    pthread_mutex_unlock(&fio_state.vfd_mutex);
    *file_id_out = file_id;
    return FIO_VFD_BASE + idx;
}

/* Give back a slot claimed by vfd_reserve whose open did not succeed. */
static void vfd_unreserve(int fd) {
    pthread_mutex_lock(&fio_state.vfd_mutex);
    fio_state.vfds[fd - FIO_VFD_BASE].active = 0;
    pthread_mutex_unlock(&fio_state.vfd_mutex);
}

static fio_vfd_t *vfd_get(int fd) {
    if (fd < FIO_VFD_BASE || fd >= FIO_VFD_BASE + FIO_MAX_FILES) return NULL;
    int idx = fd - FIO_VFD_BASE;
    if (!fio_state.vfds[idx].active) return NULL;
    return &fio_state.vfds[idx];
}

/* Virtual fds are the FIO_MAX_FILES values from FIO_VFD_BASE. Any other
 * non-negative fd didn't come from our tunnel: ffmpeg got it from the kernel
 * (pipe: dups stdin/stdout into a fresh fd; fd: takes an fd directly), so
 * read/write/seek/close/fstat on it must go to real syscalls. Server-side Go
 * plumbing already forwards stdio to/from the client (MsgStdin/MsgStdout/
 * MsgStderr). Negative fds and closed virtual fds fail with EBADF without a
 * syscall: the Windows CRT aborts on an invalid descriptor. */
static inline int is_real_fd(int fd) {
    return fd >= 0 && (fd < FIO_VFD_BASE || fd >= FIO_VFD_BASE + FIO_MAX_FILES);
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

/* Add delta to this file's and the process-wide range cache byte counts.
 * Returns the new process-wide total. */
static uint64_t range_cache_account(fio_vfd_t *vfd, int64_t delta) {
    vfd->range_cache_bytes = (uint64_t)((int64_t)vfd->range_cache_bytes + delta);
    pthread_mutex_lock(&fio_state.vfd_mutex);
    fio_state.range_cache_total = (uint64_t)((int64_t)fio_state.range_cache_total + delta);
    uint64_t total = fio_state.range_cache_total;
    pthread_mutex_unlock(&fio_state.vfd_mutex);
    return total;
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
    range_cache_account(vfd, -(int64_t)vfd->range_cache_bytes);
}

/* Only regular files: data read from a pipe or device can't be read again,
 * and keeping it would let lseek "succeed" on a pipe by replaying it. */
static int vfd_should_retain_read_cache(fio_vfd_t *vfd) {
    uint32_t accmode = vfd->wire_flags & 0x0003;
    return vfd->regular &&
           accmode == FIO_O_RDONLY &&
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

/* Drop this file's oldest blocks until the cache of every open file together
 * fits FFOIP_RANGE_CACHE_BYTES. Only the calling file's blocks are touched:
 * each file's cache belongs to the thread reading it. */
static void vfd_evict_range_cache(fio_vfd_t *vfd) {
    while (vfd->range_cache &&
           range_cache_account(vfd, 0) > fio_state.range_cache_max_bytes) {
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
        range_cache_account(vfd, -(int64_t)block->len);
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
    range_cache_account(vfd, (int64_t)len);
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
    pthread_mutex_lock(&fio_state.vfd_mutex);
    vfd->active = 0;
    pthread_mutex_unlock(&fio_state.vfd_mutex);
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
    range_cache_account(vfd, -(int64_t)block->len);

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

/* Update the known file size from a READ reply of data_len bytes that started
 * at offset start.
 *
 * Only an empty reply marks end of file. The client answers each READ with a
 * single file read, which can return fewer bytes than asked in the middle of
 * a file on network or FUSE storage, so a short reply that still carries data
 * says nothing about where the file ends and must not shrink cached_size
 * (fio_fstat and SEEK_END report it).
 *
 * A reply that reaches past cached_size means the file grew after it was
 * opened: raise cached_size to what we have seen and mark it as a lower bound,
 * so prefetch keeps reading past it (see vfd_maybe_start_prefetch). */
static void vfd_note_read_reply(fio_vfd_t *vfd, int64_t start, uint32_t data_len) {
    /* Pipes and devices have no size to learn from reads: fstat(2) keeps
     * reporting what it did at open (0 for a device), and avio_size() must
     * not turn the bytes read so far into a file length. */
    if (!vfd->regular) return;
    if (data_len == 0) {
        if (vfd->cached_size < 0 || start < vfd->cached_size) {
            vfd->cached_size = start;
        }
        return;
    }

    int64_t end = start + (int64_t)data_len;
    if (end > vfd->cached_size) {
        vfd->cached_size = end;
        vfd->size_grew = 1;
    }
}

static void vfd_update_after_prefetch_response(fio_vfd_t *vfd, uint32_t data_len) {
    vfd->remote_offset = vfd->prefetch_start + (int64_t)data_len;
    vfd_note_read_reply(vfd, vfd->prefetch_start, data_len);
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

    if (result < 0) {
        /* remote_offset still holds the optimistic end of the failed
         * prefetch; the server's real position is unknown. */
        vfd->remote_offset = -1;
    }
    vfd->prefetch_start = 0;
    free_pending(slot);
    return result;
}

static void vfd_maybe_start_prefetch(fio_vfd_t *vfd) {
    if (vfd->prefetch_slot >= 0) return;
    if (!vfd->read_cache || vfd->read_cache_len == 0) return;
    if (vfd->dirty) return;
    if (!vfd->regular) return;

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
    if (vfd->cached_size >= 0) {
        if (cache_end < vfd->cached_size) {
            int64_t remaining = vfd->cached_size - cache_end;
            if (remaining < (int64_t)request_size) {
                request_size = (uint32_t)remaining;
            }
        } else if (!vfd->size_grew) {
            /* The cache already reaches the end of the file; don't ask for
             * bytes past it. A read at EOF still goes to the server through
             * fio_read's synchronous path, which is also how a file that
             * grows while open gets noticed (vfd_note_read_reply). */
            return;
        }
    }


    uint8_t req_buf[8];
    encode_read_req(req_buf, sizeof(req_buf), 0, vfd->file_id, request_size);

    int saved_errno = errno;
    int slot = send_async(FIO_MSG_READ, req_buf, 8, 1);
    if (slot < 0) {
        errno = saved_errno;
        return;
    }

    vfd->prefetch_slot = slot;
    vfd->prefetch_start = cache_end;
    vfd->remote_offset = cache_end + (int64_t)request_size;
}

static int vfd_remote_seek(fio_vfd_t *vfd, int64_t offset, uint8_t wire_whence,
                           int64_t *new_offset) {

    uint8_t req_buf[13];
    encode_seek_req(req_buf, sizeof(req_buf), 0, vfd->file_id,
                    offset, wire_whence);

    int slot = send_and_wait(FIO_MSG_SEEK, req_buf, 13);
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
                           &decoded_offset) < 0 ||
            decoded_offset < 0 || decoded_offset > FIO_MAX_REMOTE_OFFSET) {
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
    if (result < 0 && vfd->regular) {
        /* The server's position is unknown; the next request seeks first.
         * A failed lseek on a pipe or device doesn't move it, and seeking
         * again before every read or write would only fail again. */
        vfd->remote_offset = -1;
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

static void fio_init_state(void) {
    memset(&fio_state, 0, sizeof(fio_state));
    fio_state.sock_fd = -1;
    fio_state.next_file_id = 1;
    fio_state.next_req_id = 1;
    fio_state.read_ahead_bytes = FIO_DEFAULT_READAHEAD_BYTES;
    fio_state.range_cache_max_bytes = FIO_RANGE_CACHE_MAX_BYTES;
    pthread_mutex_init(&fio_state.send_mutex, NULL);
    pthread_mutex_init(&fio_state.dispatch_mutex, NULL);
    pthread_cond_init(&fio_state.dispatch_cond, NULL);
    pthread_mutex_init(&fio_state.vfd_mutex, NULL);
}

static void fio_init_tuning(void) {
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
}

/* The tunnel was requested (FFOIP_PORT is set) but can't be used. Every
 * tunneled call then fails with EIO, as after the tunnel dies: the paths on
 * the command line are the client's, and falling back to this machine's
 * filesystem would read, write and delete the server's files instead. */
static void fio_fail_closed(void) {
    fio_state.sock_fd = -1;
    fio_state.tunnel_dead = 1;
    fio_state.initialized = 2;
}

static void fio_close_socket(int sock) {
#ifdef _WIN32
    closesocket(sock);
#else
    close(sock);
#endif
}

/* Connect to the server's loopback listener and prove we are the ffmpeg it
 * started. The server accepts any local connection on its port, so before
 * anything else fio sends FIO_HELLO_MAGIC followed by the FFOIP_TOKEN value
 * (FIO_TOKEN_LEN characters, which the server generates per process and
 * passes only in this child's environment). The server drops connections
 * that don't send it within a few seconds and serves the first that does. */
static void fio_init_tunnel(void) {
    const char *port_str = getenv("FFOIP_PORT");
    if (!port_str || port_str[0] == '\0') {
        fio_state.initialized = 1; /* passthrough */
        return;
    }

    char *end = NULL;
    errno = 0;
    long port = strtol(port_str, &end, 10);
    if (errno != 0 || !end || *end != '\0' || port <= 0 || port > 65535) {
        fprintf(stderr, "fio: invalid FFOIP_PORT=%s; file operations will fail\n", port_str);
        fio_fail_closed();
        return;
    }

    const char *token = getenv("FFOIP_TOKEN");
    if (!token || strlen(token) != FIO_TOKEN_LEN) {
        fprintf(stderr, "fio: FFOIP_PORT is set but FFOIP_TOKEN is %s; file operations will fail\n",
                token ? "malformed" : "missing");
        fio_fail_closed();
        return;
    }

#ifdef _WIN32
    WSADATA wsa;
    if (WSAStartup(MAKEWORD(2, 2), &wsa) != 0) {
        fprintf(stderr, "fio: WSAStartup failed; file operations will fail\n");
        fio_fail_closed();
        return;
    }
#endif

    int sock = socket(AF_INET, SOCK_STREAM, 0);
    if (sock < 0) {
#ifdef _WIN32
        fprintf(stderr, "fio: socket() failed: WSA error %d; file operations will fail\n", WSAGetLastError());
#else
        fprintf(stderr, "fio: socket() failed: %s; file operations will fail\n", strerror(errno));
#endif
        fio_fail_closed();
        return;
    }

    struct sockaddr_in addr;
    memset(&addr, 0, sizeof(addr));
    addr.sin_family = AF_INET;
    addr.sin_port = htons((uint16_t)port);
    addr.sin_addr.s_addr = htonl(0x7f000001); /* 127.0.0.1 */

    if (connect(sock, (struct sockaddr *)&addr, sizeof(addr)) < 0) {
#ifdef _WIN32
        fprintf(stderr, "fio: connect to 127.0.0.1:%ld failed: WSA error %d; file operations will fail\n",
                port, WSAGetLastError());
#else
        fprintf(stderr, "fio: connect to 127.0.0.1:%ld failed: %s; file operations will fail\n",
                port, strerror(errno));
#endif
        fio_close_socket(sock);
        fio_fail_closed();
        return;
    }

#ifdef SO_NOSIGPIPE
    /* macOS has no MSG_NOSIGNAL; ask for EPIPE instead of SIGPIPE here. */
    int nosigpipe = 1;
    setsockopt(sock, SOL_SOCKET, SO_NOSIGPIPE, &nosigpipe, sizeof(nosigpipe));
#endif

    /* Every request waits for its reply, so send each one at once. With
     * Nagle's algorithm on, the tail of a request could wait for the ACK
     * of its head, which the server may delay by up to 40 ms (more on
     * some systems). */
    {
        int nodelay = 1;
        setsockopt(sock, IPPROTO_TCP, TCP_NODELAY, (const char *)&nodelay, sizeof(nodelay));
    }

    uint8_t hello[FIO_HELLO_LEN];
    memcpy(hello, FIO_HELLO_MAGIC, 4);
    memcpy(hello + 4, token, FIO_TOKEN_LEN);
    if (write_all(sock, hello, sizeof(hello)) < 0) {
        fprintf(stderr, "fio: sending the tunnel token failed; file operations will fail\n");
        fio_close_socket(sock);
        fio_fail_closed();
        return;
    }

    fio_state.sock_fd = sock;

    /* Start reader thread */
    if (pthread_create(&fio_state.reader_thread, NULL, reader_thread_func, NULL) != 0) {
        fprintf(stderr, "fio: starting the reader thread failed; file operations will fail\n");
        fio_close_socket(sock);
        fio_fail_closed();
        return;
    }

    fio_state.initialized = 2; /* tunneled */
}

static void fio_init(void) {
    fio_init_state();
    fio_init_tuning();
    fio_init_tunnel();
}

static void fio_ensure_init(void) {
    pthread_once(&fio_once, fio_init);
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

    fio_init_state();
    fio_state.sock_fd = sock_fd;
    fio_state.read_ahead_bytes = read_ahead_bytes;
    fio_state.read_ahead_explicit = read_ahead_explicit;
    fio_state.range_cache_max_bytes = range_cache_max_bytes;

    if (pthread_create(&fio_state.reader_thread, NULL, reader_thread_func, NULL) != 0) {
        fio_state.sock_fd = -1;
        fio_state.initialized = 1;
        return -1;
    }
    fio_state.initialized = 2;
    return 0;
}

/* Run the real FFOIP_PORT/FFOIP_TOKEN setup again, as a fresh process would.
 * Returns fio_state.initialized (1 passthrough, 2 tunneled) and sets
 * *dead to whether the tunnel failed closed. Undo with fio_test_teardown. */
int fio_test_init_from_env(int *dead) {
    fio_ensure_init();
    fio_init_state();
    fio_init_tunnel();
    *dead = fio_state.tunnel_dead;
    return fio_state.initialized;
}

/* Whether the tunnel socket has TCP_NODELAY set. */
int fio_test_tunnel_nodelay(void) {
    int v = 0;
    socklen_t len = sizeof(v);
    if (fio_state.sock_fd < 0 ||
        getsockopt(fio_state.sock_fd, IPPROTO_TCP, TCP_NODELAY, (char *)&v, &len) < 0)
        return -1;
    return v != 0;
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

/* Report whether fd has a speculative prefetch READ in flight. */
int fio_test_prefetch_in_flight(int fd) {
    fio_vfd_t *vfd = vfd_get(fd);
    return vfd != NULL && vfd->prefetch_slot >= 0;
}

/* Request id of fd's parked prefetch, or -1 if none. */
int fio_test_prefetch_req_id(int fd) {
    fio_vfd_t *vfd = vfd_get(fd);
    if (!vfd || vfd->prefetch_slot < 0) return -1;
    pthread_mutex_lock(&fio_state.dispatch_mutex);
    int id = fio_state.pending[vfd->prefetch_slot].req_id;
    pthread_mutex_unlock(&fio_state.dispatch_mutex);
    return id;
}

void fio_test_set_next_req_id(uint16_t id) {
    pthread_mutex_lock(&fio_state.dispatch_mutex);
    fio_state.next_req_id = id;
    pthread_mutex_unlock(&fio_state.dispatch_mutex);
}

void fio_test_set_next_file_id(uint16_t id) {
    pthread_mutex_lock(&fio_state.vfd_mutex);
    fio_state.next_file_id = id;
    pthread_mutex_unlock(&fio_state.vfd_mutex);
}

int fio_test_pending_used(void) {
    pthread_mutex_lock(&fio_state.dispatch_mutex);
    int n = fio_state.pending_used;
    pthread_mutex_unlock(&fio_state.dispatch_mutex);
    return n;
}

uint64_t fio_test_range_cache_total(void) {
    pthread_mutex_lock(&fio_state.vfd_mutex);
    uint64_t n = fio_state.range_cache_total;
    pthread_mutex_unlock(&fio_state.vfd_mutex);
    return n;
}

int fio_test_max_pending(void) { return FIO_MAX_PENDING; }
int fio_test_max_files(void) { return FIO_MAX_FILES; }
uint32_t fio_test_max_io_bytes(void) { return FIO_MAX_IO_BYTES; }
#endif /* FIO_TESTING */

/* ======================================================================
 * K. Public API Functions
 * ====================================================================== */

int fio_open(const char *path, int flags, mode_t mode) {
    fio_ensure_init();

    if (fio_state.initialized == 1) {
        return fio_passthrough_open(path, flags, mode);
    }

    uint32_t wire_flags = flags_to_wire(flags);
    uint16_t wire_mode = (uint16_t)(mode & 0xFFFF);

    /* Claim the vfd before the server opens anything, so a full table can't
     * leave a file open on the server that nothing will ever close. */
    uint16_t file_id = 0;
    int fd = vfd_reserve(&file_id);
    if (fd < 0) return -1;


    uint8_t buf[4096];
    int n = encode_open_req(buf, sizeof(buf), 0, file_id, wire_flags, wire_mode, path);
    if (n < 0) {
        vfd_unreserve(fd);
        errno = ENAMETOOLONG;
        return -1;
    }

    int slot = send_and_wait(FIO_MSG_OPEN, buf, (uint32_t)n);
    if (slot < 0) {
        int saved_errno = errno;
        vfd_unreserve(fd);
        errno = saved_errno;
        return -1;
    }

    int result = -1;
    if (fio_state.pending[slot].resp_type == FIO_MSG_IO_ERROR) {
        int32_t wire_err = FIO_EIO;
        decode_io_error(fio_state.pending[slot].resp_payload,
                        fio_state.pending[slot].resp_len, &(uint16_t){0}, &wire_err);
        errno = errno_from_wire(wire_err);
    } else if (fio_state.pending[slot].resp_type == FIO_MSG_OPEN_OK) {
        int64_t file_size = 0;
        uint32_t file_mode = 0;
        if (decode_open_ok(fio_state.pending[slot].resp_payload,
                           fio_state.pending[slot].resp_len, &(uint16_t){0},
                           &file_size, &file_mode) < 0 ||
            file_size > FIO_MAX_REMOTE_OFFSET) {
            errno = EIO;
        } else {
            fio_vfd_t *vfd = &fio_state.vfds[fd - FIO_VFD_BASE];
            vfd->wire_flags = wire_flags;
            vfd->wire_mode = file_mode;
            vfd->regular = S_ISREG(mode_from_wire(file_mode));
            vfd->cached_size = file_size;
            result = fd;
        }
    } else {
        errno = EIO;
    }

    free_pending(slot);
    if (result < 0) {
        int saved_errno = errno;
        vfd_unreserve(fd);
        errno = saved_errno;
    }
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
            /* An empty prefetch means end of file, unless the file is
             * growing: the writer may have appended since the prefetch was
             * answered, so ask again, as read(2) would see the new data. */
            if (!vfd->size_grew) return 0;
        } else {
            /* Cancelling keeps the prefetched data in the range cache, which
             * may hold the bytes asked for. */
            vfd_cancel_prefetch(vfd);
            cached = vfd_copy_from_cache(vfd, buf, count);
            if (cached > 0) {
                vfd_maybe_start_prefetch(vfd);
                return cached;
            }
        }
    }

    if (vfd_sync_remote_offset(vfd) < 0) return -1;
    if (at_cache_end) {
        vfd_grow_read_ahead(vfd);
    }


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
    if (request_size > FIO_MAX_IO_BYTES) request_size = FIO_MAX_IO_BYTES;

    uint8_t req_buf[8];
    encode_read_req(req_buf, sizeof(req_buf), 0, vfd->file_id, request_size);

    int slot = send_and_wait(FIO_MSG_READ, req_buf, 8);
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
                           fio_state.pending[slot].resp_len, &(uint16_t){0}, &data, &data_len) < 0 ||
            data_len > request_size) {
            errno = EIO;
            result = -1;
        } else {
            int64_t response_start = vfd->logical_offset;
            vfd->remote_offset += (int64_t)data_len;
            vfd_note_read_reply(vfd, response_start, data_len);

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
    if (result < 0 && vfd->regular) {
        /* Where the server's file position ended up is unknown; the next
         * request seeks first. (Pipes can't seek and don't need to.) */
        vfd->remote_offset = -1;
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


    if (count > FIO_MAX_IO_BYTES) count = FIO_MAX_IO_BYTES; /* short write */
    uint32_t payload_len = 4 + (uint32_t)count;
    uint8_t *req_buf = malloc(payload_len);
    if (!req_buf) { errno = ENOMEM; return -1; }

    encode_write_req(req_buf, payload_len, 0, vfd->file_id,
                     (const uint8_t *)buf, (uint32_t)count);

    int slot = send_and_wait(FIO_MSG_WRITE, req_buf, payload_len);
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
                            fio_state.pending[slot].resp_len, &(uint16_t){0}, &written) < 0 ||
            written > count) {
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
    if (result < 0 && vfd->regular) {
        /* A failed write may still have written part of the data (the
         * client reports that as a short write, but don't rely on it): the
         * next request seeks to where fio thinks the file position is. */
        vfd->remote_offset = -1;
    }

    free_pending(slot);
    return result;
}

/* *out = base + off, or -1 with errno set as lseek(2) does when the sum
 * doesn't fit: EOVERFLOW above the largest offset, EINVAL below zero. */
static int offset_add(int64_t base, int64_t off, int64_t *out) {
    if (off > 0 && base > INT64_MAX - off) {
#ifdef EOVERFLOW
        errno = EOVERFLOW;
#else
        errno = EINVAL;
#endif
        return -1;
    }
    if (off < 0 && base < INT64_MIN - off) {
        errno = EINVAL;
        return -1;
    }
    *out = base + off;
    return 0;
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

    if (!vfd->regular) {
        /* A pipe or device: whatever the client's lseek says (ESPIPE for a
         * pipe), as a local lseek would. Nothing fio holds can be seeked
         * within. */
        int64_t target = 0;
        uint8_t wire_whence = FIO_SEEK_SET;
        if (whence == SEEK_SET) {
            target = (int64_t)offset;
        } else if (whence == SEEK_CUR) {
            if (offset_add(vfd->logical_offset, (int64_t)offset, &target) < 0) return -1;
        } else if (whence == SEEK_END) {
            target = (int64_t)offset;
            wire_whence = FIO_SEEK_END;
        } else {
            errno = EINVAL;
            return -1;
        }
        if (wire_whence == FIO_SEEK_SET && target < 0) {
            errno = EINVAL;
            return -1;
        }
        int64_t new_offset = 0;
        if (vfd_remote_seek(vfd, target, wire_whence, &new_offset) < 0) return -1;
        vfd->logical_offset = new_offset;
        vfd_invalidate_read_cache(vfd);
        return (off_t)new_offset;
    }

    int64_t target = 0;
    switch (whence) {
    case SEEK_SET:
        target = (int64_t)offset;
        break;
    case SEEK_CUR:
        if (offset_add(vfd->logical_offset, (int64_t)offset, &target) < 0) return -1;
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
        if (offset_add(vfd->cached_size, (int64_t)offset, &target) < 0) return -1;
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
    /* The cancelled prefetch's data went to the range cache. */
    if (vfd_cache_contains(vfd, target)) {
        vfd->logical_offset = target;
        return (off_t)target;
    }
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


    uint8_t req_buf[4];
    encode_close_req(req_buf, sizeof(req_buf), 0, vfd->file_id);

    int slot = send_and_wait(FIO_MSG_CLOSE, req_buf, 4);
    if (slot < 0) {
        /* Like close(2), the descriptor is released even when closing
         * fails; callers never retry a close. */
        int saved_errno = errno;
        vfd_free(fd);
        errno = saved_errno;
        return -1;
    }

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

    /* Not written to since open: the size and type from OPEN_OK (and later
     * reads) still hold. The type matters: file.c marks pipes as streamed. */
    if (!vfd->dirty) {
        memset(st, 0, sizeof(*st));
        st->st_size = vfd->cached_size;
        st->st_mode = mode_from_wire(vfd->wire_mode);
        return 0;
    }


    uint8_t req_buf[4];
    encode_fstat_req(req_buf, sizeof(req_buf), 0, vfd->file_id);

    int slot = send_and_wait(FIO_MSG_FSTAT, req_buf, 4);
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
            st->st_mode = mode_from_wire(mode);
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


    uint8_t req_buf[12];
    encode_ftruncate_req(req_buf, sizeof(req_buf), 0, vfd->file_id, (int64_t)length);

    int slot = send_and_wait(FIO_MSG_FTRUNCATE, req_buf, 12);
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


    uint8_t buf[4096];
    int n = encode_unlink_req(buf, sizeof(buf), 0, path);
    if (n < 0) { errno = ENAMETOOLONG; return -1; }

    int slot = send_and_wait(FIO_MSG_UNLINK, buf, (uint32_t)n);
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


    uint8_t buf[8192];
    int n = encode_rename_req(buf, sizeof(buf), 0, oldpath, newpath);
    if (n < 0) { errno = ENAMETOOLONG; return -1; }

    int slot = send_and_wait(FIO_MSG_RENAME, buf, (uint32_t)n);
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
#if defined(_WIN32) && !defined(HAVE_AV_CONFIG_H)
        (void)mode;
        return _mkdir(path);
#else
        return mkdir(path, mode); /* os_support.h's UTF-8 wrapper on Windows */
#endif
    }


    uint8_t buf[4096];
    int n = encode_mkdir_req(buf, sizeof(buf), 0, (uint16_t)(mode & 0xFFFF), path);
    if (n < 0) { errno = ENAMETOOLONG; return -1; }

    int slot = send_and_wait(FIO_MSG_MKDIR, buf, (uint32_t)n);
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

/* ======================================================================
 * L. Path queries: stat, access check, directory listing
 *
 * When tunneled these always ask the client: the server only accepts a
 * client of its own protocol version, which handles every request type.
 * ====================================================================== */

int fio_tunneled(void) {
    fio_ensure_init();
    return fio_state.initialized == 2;
}

/* Send a request whose payload is req_id + path and wait for the reply.
 * Returns the pending slot (the caller must free_pending it), or -1 with errno
 * set. */
static int send_path_request(uint8_t msg_type, const char *path) {

    uint8_t buf[2 + FIO_PATH_MAX];
    int n = encode_path_req(buf, sizeof(buf), 0, path);
    if (n < 0) { errno = ENAMETOOLONG; return -1; }
    return send_and_wait(msg_type, buf, (uint32_t)n);
}

/* Set errno from an IO_ERROR (or unexpected) reply in slot. */
static void set_errno_from_reply(int slot) {
    if (fio_state.pending[slot].resp_type == FIO_MSG_IO_ERROR) {
        int32_t wire_err = FIO_EIO;
        decode_io_error(fio_state.pending[slot].resp_payload,
                        fio_state.pending[slot].resp_len, &(uint16_t){0}, &wire_err);
        errno = errno_from_wire(wire_err);
    } else {
        errno = EIO;
    }
}

static int remote_stat(const char *path, int64_t *size, uint32_t *mode,
                       int64_t *mtime, uint8_t *access_bits) {
    int slot = send_path_request(FIO_MSG_STAT, path);
    if (slot < 0) return -1;

    int result = -1;
    if (fio_state.pending[slot].resp_type == FIO_MSG_STAT_OK &&
        decode_stat_ok(fio_state.pending[slot].resp_payload,
                       fio_state.pending[slot].resp_len, &(uint16_t){0},
                       size, mode, mtime, access_bits) == 0) {
        result = 0;
    } else {
        set_errno_from_reply(slot);
    }
    free_pending(slot);
    return result;
}

int fio_stat(const char *path, struct stat *st) {
    fio_ensure_init();

    if (fio_state.initialized == 1) {
        return stat(path, st); /* os_support.h's UTF-8 wrapper on Windows */
    }

    int64_t size = 0, mtime = 0;
    uint32_t mode = 0;
    uint8_t access_bits = 0;
    if (remote_stat(path, &size, &mode, &mtime, &access_bits) < 0) return -1;

    memset(st, 0, sizeof(*st));
    st->st_size  = size;
    st->st_mode  = mode_from_wire(mode);
    st->st_mtime = (time_t)mtime;
    return 0;
}

static int local_check(const char *path) {
    if (access(path, 0) < 0) return -1;
    int granted = 0;
    if (access(path, R_OK) == 0) granted |= FIO_R_OK;
    if (access(path, W_OK) == 0) granted |= FIO_W_OK;
#ifndef _WIN32
    if (access(path, X_OK) == 0) granted |= FIO_X_OK;
#endif
    return granted;
}

int fio_check(const char *path) {
    fio_ensure_init();

    if (fio_state.initialized == 1) {
        return local_check(path);
    }

    int64_t size = 0, mtime = 0;
    uint32_t mode = 0;
    uint8_t access_bits = 0;
    if (remote_stat(path, &size, &mode, &mtime, &access_bits) < 0) return -1;

    int granted = 0;
    if (access_bits & FIO_ACCESS_R) granted |= FIO_R_OK;
    if (access_bits & FIO_ACCESS_W) granted |= FIO_W_OK;
    if (access_bits & FIO_ACCESS_X) granted |= FIO_X_OK;
    return granted;
}

struct fio_dir {
    int      remote;
    /* remote: the ListDirOk payload, walked entry by entry */
    uint8_t *payload;
    uint32_t payload_len;
    uint32_t pos;
    uint32_t remaining;
    /* name of the entry last returned */
    char    *name;
    size_t   name_cap;
#ifndef _WIN32
    DIR     *dir;
    char    *path;
#endif
};

static int fio_dir_set_name(fio_dir *d, const char *name, size_t len) {
    if (len + 1 > d->name_cap) {
        char *grown = realloc(d->name, len + 1);
        if (!grown) { errno = ENOMEM; return -1; }
        d->name = grown;
        d->name_cap = len + 1;
    }
    memcpy(d->name, name, len);
    d->name[len] = '\0';
    return 0;
}

static int dirent_type_from_wire(uint32_t mode) {
    switch (mode & FIO_S_IFMT) {
    case FIO_S_IFREG:  return FIO_DT_FILE;
    case FIO_S_IFDIR:  return FIO_DT_DIR;
    case FIO_S_IFLNK:  return FIO_DT_LINK;
    case FIO_S_IFIFO:  return FIO_DT_FIFO;
    case FIO_S_IFCHR:  return FIO_DT_CHR;
    case FIO_S_IFBLK:  return FIO_DT_BLK;
    case FIO_S_IFSOCK: return FIO_DT_SOCK;
    default:           return FIO_DT_UNKNOWN;
    }
}

static fio_dir *local_opendir(const char *path) {
#ifdef _WIN32
    (void)path;
    errno = ENOSYS;
    return NULL;
#else
    fio_dir *d = calloc(1, sizeof(*d));
    if (!d) { errno = ENOMEM; return NULL; }
    d->path = strdup(path);
    d->dir = d->path ? opendir(path) : NULL;
    if (!d->dir) {
        int saved_errno = d->path ? errno : ENOMEM;
        free(d->path);
        free(d);
        errno = saved_errno;
        return NULL;
    }
    return d;
#endif
}

fio_dir *fio_opendir(const char *path) {
    fio_ensure_init();

    if (fio_state.initialized == 1) {
        return local_opendir(path);
    }

    int slot = send_path_request(FIO_MSG_LISTDIR, path);
    if (slot < 0) return NULL;

    fio_dir *d = NULL;
    uint32_t count = 0;
    if (fio_state.pending[slot].resp_type == FIO_MSG_LISTDIR_OK &&
        decode_listdir_header(fio_state.pending[slot].resp_payload,
                              fio_state.pending[slot].resp_len,
                              &(uint16_t){0}, &count) >= 0) {
        d = calloc(1, sizeof(*d));
        if (!d) {
            errno = ENOMEM;
        } else {
            d->remote      = 1;
            d->payload     = fio_state.pending[slot].resp_payload;
            d->payload_len = fio_state.pending[slot].resp_len;
            d->pos         = 6;
            d->remaining   = count;
            fio_state.pending[slot].resp_payload = NULL;
        }
    } else {
        set_errno_from_reply(slot);
    }
    free_pending(slot);
    return d;
}

int fio_readdir(fio_dir *d, fio_dirent *e) {
    if (d->remote) {
        while (d->remaining > 0) {
            const uint8_t *name = NULL;
            uint16_t name_len = 0;
            uint32_t mode = 0;
            int64_t size = 0, mtime = 0;
            if (decode_listdir_entry(d->payload, d->payload_len, &d->pos,
                                     &name, &name_len, &mode, &size, &mtime) < 0) {
                d->remaining = 0;
                errno = EIO;
                return -1;
            }
            d->remaining--;

            /* Never hand out names a local readdir() could not produce:
             * callers join them onto the directory path. */
            if (name_len == 0 || memchr(name, '\0', name_len) ||
                memchr(name, '/', name_len) ||
#ifdef _WIN32
                memchr(name, '\\', name_len) ||
#endif
                (name_len == 1 && name[0] == '.') ||
                (name_len == 2 && name[0] == '.' && name[1] == '.')) {
                continue;
            }
            if (fio_dir_set_name(d, (const char *)name, name_len) < 0) return -1;

            memset(e, 0, sizeof(*e));
            e->name     = d->name;
            e->has_stat = 1;
            e->type     = dirent_type_from_wire(mode);
            e->perm     = mode & 0777;
            e->size     = size;
            e->mtime    = mtime;
            return 1;
        }
        return 0;
    }

#ifdef _WIN32
    (void)e;
    errno = ENOSYS;
    return -1;
#else
    for (;;) {
        errno = 0;
        struct dirent *de = readdir(d->dir);
        if (!de) return errno ? -1 : 0;
        if (!strcmp(de->d_name, ".") || !strcmp(de->d_name, "..")) continue;
        if (fio_dir_set_name(d, de->d_name, strlen(de->d_name)) < 0) return -1;

        memset(e, 0, sizeof(*e));
        e->name = d->name;

        size_t full_len = strlen(d->path) + 1 + strlen(de->d_name) + 1;
        char *full = malloc(full_len);
        struct stat st;
        if (full) {
            snprintf(full, full_len, "%s/%s", d->path, de->d_name);
            if (lstat(full, &st) == 0) {
                e->has_stat = 1;
                if (S_ISREG(st.st_mode))       e->type = FIO_DT_FILE;
                else if (S_ISDIR(st.st_mode))  e->type = FIO_DT_DIR;
                else if (S_ISLNK(st.st_mode))  e->type = FIO_DT_LINK;
                else if (S_ISFIFO(st.st_mode)) e->type = FIO_DT_FIFO;
                else if (S_ISCHR(st.st_mode))  e->type = FIO_DT_CHR;
                else if (S_ISBLK(st.st_mode))  e->type = FIO_DT_BLK;
                else if (S_ISSOCK(st.st_mode)) e->type = FIO_DT_SOCK;
                e->perm  = (unsigned)(st.st_mode & 0777);
                e->size  = (int64_t)st.st_size;
                e->mtime = (int64_t)st.st_mtime;
                e->atime = (int64_t)st.st_atime;
                e->ctime = (int64_t)st.st_ctime;
                e->uid   = (int64_t)st.st_uid;
                e->gid   = (int64_t)st.st_gid;
            }
            free(full);
        }
        return 1;
    }
#endif
}

int fio_closedir(fio_dir *d) {
    if (!d) return 0;
#ifndef _WIN32
    if (d->dir) closedir(d->dir);
    free(d->path);
#endif
    free(d->payload);
    free(d->name);
    free(d);
    return 0;
}
