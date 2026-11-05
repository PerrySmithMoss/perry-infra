# Candidate for a stack that already starts PostgreSQL directly as UID 999.
# Root startup is deliberately unsupported.
ARG POSTGRES_BASE=postgres:17-bookworm
FROM ${POSTGRES_BASE}
RUN rm /usr/local/bin/gosu
USER 999:999
