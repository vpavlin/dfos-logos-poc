#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "dfos.h"

static void stub_waku_publish(const char* topic, const char* payload) {
    printf("[waku] topic=%s payload=%.80s...\n", topic, payload);
}

static const char* stub_codex_upload(const char* data, int len) {
    printf("[codex] upload %d bytes\n", len);
    return "bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi";
}

static const char* stub_codex_download(const char* cid) {
    printf("[codex] download cid=%s\n", cid);
    return "{\"type\":\"post/v1\",\"text\":\"stub\",\"createdAt\":\"2026-01-01T00:00:00.000Z\"}";
}

int main(void) {
    char* err = dfos_init("/tmp/dfos-smoke-test", stub_waku_publish, stub_codex_upload, stub_codex_download);
    if (err) {
        fprintf(stderr, "dfos_init failed: %s\n", err);
        dfos_free(err);
        return 1;
    }
    printf("dfos_init OK\n");

    char* id_json = dfos_create_identity();
    printf("dfos_create_identity: %s\n", id_json);
    dfos_free(id_json);

    char* get_json = dfos_get_identity();
    printf("dfos_get_identity: %s\n", get_json);
    dfos_free(get_json);

    char* post_json = dfos_publish_post("Hello from DFOS on Logos!");
    printf("dfos_publish_post: %s\n", post_json);
    dfos_free(post_json);

    char* feed_json = dfos_get_feed(10);
    printf("dfos_get_feed: %s\n", feed_json);
    dfos_free(feed_json);

    return 0;
}
