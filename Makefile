.PHONY: test test-integration lint seed seed-policies

test:  ## unit tests: the manifest, the OpenAPI document and the routes, merge safety, the lifecycle tables
	go vet ./... && go test ./...

test-integration:  ## L2 against PostgreSQL and the bus (BE_SDK_TEST_PG_ADMIN, BE_SDK_TEST_NATS_URL)
	go test -count=1 -tags integration ./itest/

lint:
	brickkit lint

seed-policies:  ## the policies only, before any movement of a legal entity is valued (PO01; the root seed-data runs it first)
	$(MAKE) -C ../../mdm/org seed   # the legal entities
	go run ./cmd/seed policies

seed:  ## the seed data for people, through the running component's user surface (SEED_PASSWORD; see cmd/seed)
	$(MAKE) seed-policies           # the methods are set before the dependencies' seeds make the movements
	$(MAKE) -C ../../mdm/currency seed   # the declared dependencies; the rest come through them (seed and test data conventions)
	$(MAKE) -C ../inventory seed
	$(MAKE) -C ../purchase seed
	$(MAKE) -C ../finance seed
	go run ./cmd/seed
