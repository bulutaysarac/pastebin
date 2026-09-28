.PHONY: build run stop remove

build: ## Build the app image
	docker compose build

run: ## Start app + MySQL in the background (rebuilds the image if code changed)
	docker compose up -d --build
	@echo "pastebin is up on http://localhost:8080"

stop: ## Stop the containers without deleting anything
	docker compose stop

remove: ## Delete containers, network, the MySQL data volume and the app image
	docker compose down --volumes --rmi local
