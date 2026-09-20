/*
 * Start a selected application with its routing group, then permanently drop
 * root. The caller validates the canonical bundle and the originating UID.
 * Responsibility attribution follows vpnonly's MIT-licensed launcher pattern;
 * see THIRD_PARTY.md and Packaging/Licenses/vpnonly-MIT.txt.
 */
#include <errno.h>
#include <grp.h>
#include <pwd.h>
#include <spawn.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

extern char **environ;
extern int responsibility_spawnattrs_setdisclaim(posix_spawnattr_t *, int)
    __attribute__((weak_import));

int main(int argc, char **argv) {
    if (argc != 4 || geteuid() != 0 || argv[3][0] != '/') return 2;
    char *end = NULL;
    unsigned long raw_uid = strtoul(argv[1], &end, 10);
    if (!end || *end || raw_uid < 501 || raw_uid > 0xfffffffeUL) return 2;
    unsigned long raw_gid = strtoul(argv[2], &end, 10);
    if (!end || *end || raw_gid < 61000 || raw_gid >= 65000) return 2;
    uid_t uid = (uid_t)raw_uid;
    gid_t gid = (gid_t)raw_gid;
    struct passwd *pw = getpwuid(uid);
    if (!pw) return 2;
    char *user = strdup(pw->pw_name), *home_dir = strdup(pw->pw_dir);
    char *shell = strdup(pw->pw_shell && *pw->pw_shell ? pw->pw_shell : "/bin/zsh");
    gid_t original_gid = pw->pw_gid;
    if (!user || !home_dir || !shell) return 2;
    struct group *group = getgrgid(gid);
    char prefix[40]; snprintf(prefix, sizeof(prefix), "goc_%lu_", raw_uid);
    if (!group || strncmp(group->gr_name, prefix, strlen(prefix)) != 0) return 2;

    // Preserve the user's ordinary file access; the effective primary group is
    // the routing label. No membership or privilege is added to the account.
    if (initgroups(user, original_gid) != 0 || setgid(gid) != 0 || setuid(uid) != 0) return 3;
    if (getuid() != uid || geteuid() != uid || getgid() != gid || getegid() != gid) return 3;
    if (setenv("HOME", home_dir, 1) || setenv("USER", user, 1) ||
        setenv("LOGNAME", user, 1) || setenv("SHELL", shell, 1) ||
        setenv("PATH", "/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin", 1)) return 3;
    size_t size = confstr(_CS_DARWIN_USER_TEMP_DIR, NULL, 0);
    if (size > 1) {
        char *temp = malloc(size);
        if (!temp || confstr(_CS_DARWIN_USER_TEMP_DIR, temp, size) == 0 || setenv("TMPDIR", temp, 1)) return 3;
        free(temp);
    }
    if (chdir(home_dir) != 0) return 3;
    posix_spawnattr_t attrs;
    if (posix_spawnattr_init(&attrs) != 0 || posix_spawnattr_setflags(&attrs, POSIX_SPAWN_SETEXEC) != 0) return 4;
    if (responsibility_spawnattrs_setdisclaim) responsibility_spawnattrs_setdisclaim(&attrs, 1);
    char *args[] = {argv[3], NULL};
    int result = posix_spawn(NULL, argv[3], NULL, &attrs, args, environ);
    fprintf(stderr, "Application launch failed: %s\n", strerror(result));
    return 4;
}
