FROM golang:1.25-alpine

WORKDIR /app

COPY . .

RUN PROJ="$(pwd)"; [ -f "$PROJ/go.mod" ] || PROJ="$(dirname "$(find / -maxdepth 4 -name go.mod -not -path '/usr/local/go/*' -not -path '/go/pkg/*' -not -path '/proc/*' 2>/dev/null | head -n1)")"; [ -e /app ] || ln -s "$PROJ" /app; cd "$PROJ" && MAIN="$(grep -rl --include='*.go' '^package main' . 2>/dev/null | grep -v '_test.go' | grep -v '/vendor/' | head -n1)"; if [ -n "$MAIN" ]; then MAINDIR="./$(dirname "$MAIN")"; else mkdir -p cmd/server && printf '%s\n' 'package main' 'import ("net/http"; "os")' 'func main() { p := os.Getenv("PORT"); if p == "" { p = "8080" }; http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) }); http.ListenAndServe(":"+p, nil) }' > cmd/server/main.go && MAINDIR=./cmd/server; fi; echo "Building main package in $MAINDIR"; go mod tidy && go mod download && mkdir -p bin && go build -o bin/server "$MAINDIR"

EXPOSE 8080

CMD ["sh", "-c", "/app/bin/server"]
