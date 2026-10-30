build:
	docker build -t golang_image:1 .
run:
	docker run --rm -p 4000:4000 --name forum_container golang_image:1
