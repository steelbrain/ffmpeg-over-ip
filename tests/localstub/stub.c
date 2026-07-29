/* stub.c — a stand-in for ffmpeg that reports how fio served each file.
 *
 * The local-prefix allowlist is invisible from the outside: a tunneled read
 * and a local read produce identical bytes, so a deployment where the prefix
 * silently fails to match still "works", just slower. This stub makes the
 * decision observable without waiting on a full jellyfin-ffmpeg build.
 *
 * Install it as `ffmpeg` (or `ffprobe`) next to ffmpeg-over-ip-server and the
 * server will exec it for real client requests, exercising the entire path:
 * client -> wire -> server -> exec -> fio_open -> local disk or tunnel.
 *
 * Build:
 *   cc -O2 -o ffmpeg tests/localstub/stub.c fio/fio.c -Ifio -lpthread
 *
 * Usage mirrors the shape of an ffmpeg invocation; every argument that is not
 * a flag (and is not the value of -i) is treated as a path to probe:
 *   ffmpeg -i /media/movies/a.mkv
 */

#include <fcntl.h>
#include <stdio.h>
#include <string.h>
#include <unistd.h>

#include "fio.h"

/* Tunneled files get virtual descriptors at FIO_VFD_BASE and above; anything
 * below that is a real kernel fd, which means fio opened it on this host. */
#define VFD_BASE 10000

static int probe(const char *path) {
    int fd = fio_open(path, O_RDONLY, 0);
    if (fd < 0) {
        printf("MISS   %s (open failed)\n", path);
        return 1;
    }

    char buf[64];
    memset(buf, 0, sizeof(buf));
    ssize_t n = fio_read(fd, buf, sizeof(buf) - 1);
    for (ssize_t i = 0; i < n; i++) {
        if (buf[i] == '\n' || buf[i] == '\r') buf[i] = ' ';
    }

    printf("%-6s %s (fd=%d, %zd bytes) %s\n",
           fd >= VFD_BASE ? "TUNNEL" : "LOCAL", path, fd, n, n > 0 ? buf : "");
    fio_close(fd);
    return 0;
}

int main(int argc, char **argv) {
    int failures = 0, probed = 0;

    for (int i = 1; i < argc; i++) {
        if (argv[i][0] == '-') {
            /* -i takes a path; every other flag is ignored along with nothing,
             * since we only care about operands here. */
            if (strcmp(argv[i], "-i") == 0 && i + 1 < argc) {
                failures += probe(argv[++i]);
                probed++;
            }
            continue;
        }
        failures += probe(argv[i]);
        probed++;
    }

    if (probed == 0) {
        printf("stub: no paths given\n");
        return 2;
    }
    return failures == 0 ? 0 : 1;
}
