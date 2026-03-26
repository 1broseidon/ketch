package extract

import "testing"

func TestDetectJSShell(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		html string
		want string
	}{
		{
			name: "minimal real content is static",
			html: `
				<!doctype html>
				<html>
					<head><title>Article</title></head>
					<body>
						<main>
							<article>
								<h1>Shipping Notes</h1>
								<p>This page has actual content for extraction and is meant to look like a conventional server-rendered article rather than a JavaScript bootstrap shell with placeholders.</p>
								<p>The second paragraph adds enough visible text to exceed the threshold, which should cause the detector to classify the document as static even if the markup is otherwise minimal.</p>
							</article>
						</main>
					</body>
				</html>
			`,
			want: "static",
		},
		{
			name: "salesforce shell is likely shell",
			html: `
				<!doctype html>
				<html>
					<head>
						<title>Lightning</title>
						<script>window.__BOOTSTRAP__ = {"routes":["a","b","c"],"payload":"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"};</script>
						<script src="/assets/app.js"></script>
					</head>
					<body>
						<div id="app">Loading</div>
						<noscript>This app requires JavaScript and redirects when JavaScript is disabled.</noscript>
					</body>
				</html>
			`,
			want: "likely_shell",
		},
		{
			name: "react spa shell is likely shell",
			html: `
				<!doctype html>
				<html>
					<head>
						<title>App</title>
						<script id="__NEXT_DATA__" type="application/json">
							{"buildId":"dev","page":"/","props":{"chunks":["aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"]}}
						</script>
					</head>
					<body>
						<div id="root"></div>
					</body>
				</html>
			`,
			want: "likely_shell",
		},
		{
			name: "short real page is ambiguous",
			html: `
				<!doctype html>
				<html>
					<head><title>Not Found</title></head>
					<body>
						<main>
							<h1>Not Found</h1>
							<p>The page you requested could not be located.</p>
						</main>
					</body>
				</html>
			`,
			want: "ambiguous",
		},
		{
			name: "js loading page with fallback description is likely shell",
			html: `
				<!doctype html>
				<html>
					<head><title>Flowchart Maker</title></head>
					<body>
						<div id="geInfo">
							<h1>Flowchart Maker and Online Diagram Software</h1>
							<p>draw.io is free online diagram software. You can use it as a flowchart maker, network diagram software, to create UML online, as an ER diagram tool, to design database schema, and more.</p>
							<h2>Loading... <img src="spin.gif"/></h2>
							<p>Please ensure JavaScript is enabled.</p>
						</div>
						<script src="js/main.js"></script>
					</body>
				</html>
			`,
			want: "likely_shell",
		},
		{
			name: "ssr next page with real content is static",
			html: `
				<!doctype html>
				<html>
					<head>
						<title>SSR Next</title>
						<script id="__NEXT_DATA__" type="application/json">
							{"page":"/docs","props":{"pageProps":{"title":"SSR"}}}
						</script>
					</head>
					<body>
						<div id="__next">
							<main>
								<article>
									<h1>Rendered Content</h1>
									<p>The initial HTML already includes the full article body, so the detector should treat the document as static even though the page carries the standard Next.js bootstrap data.</p>
									<p>This extra paragraph ensures there is comfortably more than two hundred characters of visible text in the extraction selectors and prevents a false positive.</p>
								</article>
							</main>
						</div>
					</body>
				</html>
			`,
			want: "static",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := DetectJSShell(tt.html); got != tt.want {
				t.Fatalf("DetectJSShell() = %q, want %q", got, tt.want)
			}
		})
	}
}
