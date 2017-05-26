FROM registry.hub.docker.com/library/golang

ADD vcsim vcsim

CMD ["./vcsim", "-httptest.serve", "0.0.0.0:8989", "-trace"]
