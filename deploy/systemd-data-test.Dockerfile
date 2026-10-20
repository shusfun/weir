# Local-only acceptance image: reuse cached images and PG18 musl client tools.
FROM postgres:18-alpine AS pgtools
FROM migate-systemd-test:bookworm
COPY --from=pgtools /usr/local/bin/pg_dump /opt/pg18/bin/pg_dump
COPY --from=pgtools /usr/local/bin/pg_restore /opt/pg18/bin/pg_restore
COPY --from=pgtools /usr/local/lib /opt/pg18/lib
COPY --from=pgtools /usr/lib /opt/pg18/usr-lib
COPY --from=pgtools /lib /opt/pg18/alpine-lib
RUN printf '%s\n' '#!/bin/sh' 'exec /opt/pg18/alpine-lib/ld-musl-x86_64.so.1 --library-path /opt/pg18/lib:/opt/pg18/usr-lib:/opt/pg18/alpine-lib /opt/pg18/bin/pg_dump "$@"' > /usr/local/bin/pg_dump \
 && printf '%s\n' '#!/bin/sh' 'exec /opt/pg18/alpine-lib/ld-musl-x86_64.so.1 --library-path /opt/pg18/lib:/opt/pg18/usr-lib:/opt/pg18/alpine-lib /opt/pg18/bin/pg_restore "$@"' > /usr/local/bin/pg_restore \
 && chmod 755 /usr/local/bin/pg_dump /usr/local/bin/pg_restore \
 && pg_dump --version && pg_restore --version
