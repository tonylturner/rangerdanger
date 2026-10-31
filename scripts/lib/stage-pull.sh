#!/usr/bin/env bash

# RD_STAGE_PULL_ATTEMPTS and RD_STAGE_PULL_DELAY_S override retry limits for tests.
stage_pull_image() {
    local image="$1" ref="$2"
    local attempts="${RD_STAGE_PULL_ATTEMPTS:-3}" delay_override="${RD_STAGE_PULL_DELAY_S:-}"
    local attempt=1 delay rc

    case "$attempts" in
        1|2|3) ;;
        *) attempts=3 ;;
    esac
    case "$delay_override" in
        0|1|2|3|4|5|6|7|8|9) ;;
        *) delay_override="" ;;
    esac

    case "$image" in
        *rangerdanger-corp-ws*|*rangerdanger-eng-ws*|*rangerdanger-vendor-jump*)
            say "    large image (~2-3 GB desktop) — a few minutes is normal" ;;
        *rangerdanger-openplc*)
            say "    large image (~1 GB) — give it a minute" ;;
    esac

    while [ "$attempt" -le "$attempts" ]; do
        if docker pull "$ref"; then
            return 0
        else
            rc=$?
        fi
        if [ "$attempt" -eq "$attempts" ]; then
            return "$rc"
        fi
        if [ -n "$delay_override" ]; then
            delay="$delay_override"
        elif [ "$attempt" -eq 1 ]; then
            delay=5
        else
            delay=15
        fi
        say "docker pull attempt $attempt/$attempts failed for $ref; retrying in ${delay}s after registry/pull error"
        sleep "$delay"
        attempt=$((attempt + 1))
    done
    return 1
}
