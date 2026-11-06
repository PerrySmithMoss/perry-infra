# Candidate for a stack that already starts PostgreSQL directly as UID 999.
# Root startup is deliberately unsupported.
FROM postgres:17.11-bookworm@sha256:665dc7b37ac52a17e298511414c21d23ffa4e6a6ef6225b3e5d59c7afb35e994
# GnuPG was used to build the base image. PostgreSQL does not need it or SQLite.
RUN apt-get purge -y --no-auto-remove --no-download gpg libsqlite3-0 && \
    rm /usr/local/bin/gosu
USER 999:999
