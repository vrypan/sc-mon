SCMON_VERSION := $(shell git describe --tags 2>/dev/null || echo "v0.0.0")

BINS = sc-mon
SCMON_SOURCES := $(wildcard */*.go go.mod)

# Colors for output
GREEN = \033[0;32m
NC = \033[0m

all: $(BINS)

clean:
	@echo -e "$(GREEN)Deleting sc-mon binary...$(NC)"
	rm -f $(BINS)

.PHONY: all clean local tag tag-minor tag-major releases

sc-mon: $(SCMON_SOURCES)
	@echo -e "$(GREEN)Building sc-mon ${SCMON_VERSION} $(NC)"
	go build -o $@ -ldflags "-w -s -X main.SCMON_VERSION=${SCMON_VERSION}"

tag:
	./bin/auto_increment_tag.sh patch

tag-minor:
	./bin/auto_increment_tag.sh minor

tag-major:
	./bin/auto_increment_tag.sh major

releases:
	goreleaser release --clean
