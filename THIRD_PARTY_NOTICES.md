# Third-Party Notices

Crew Studio includes or depends on the third-party software listed below. Each component is used under its own license; the full text of every license named here appears once in [License texts](#license-texts), and the notices that Apache-2.0 components require to be reproduced appear in [Apache NOTICE files](#apache-notice-files).

This file covers what ships in a build of the product:

- **Go server binary** (`studio/server`, `go build ./cmd/crewstudio`): the Go standard library and the Go modules linked into the binary.
- **Web UI production bundle** (`studio/web`, `npm run build`): the npm packages whose code or CSS ends up in `dist/`. Packages that were tree-shaken out, and all devDependencies (Vite, TypeScript, Vitest, Tailwind tooling, Playwright), are not shipped and are not listed, except where they inject code or CSS into the bundle (noted below).
- **Python runner** (`studio/runner`): its dependencies are not redistributed in this repository; `studio/runner/requirements.txt` pins them and they are installed by the operator from PyPI. They are listed for information in [Python runner dependencies](#python-runner-dependencies-not-redistributed).

Copyright lines are taken from each component's LICENSE/NOTICE file or, where that file has none, from its source headers or package metadata.

## Go server binary

| Component | Version | License | Copyright |
|---|---|---|---|
| Go standard library and runtime | go1.24.2 | BSD-3-Clause | Copyright 2009 The Go Authors |
| cloud.google.com/go/compute/metadata | v0.3.0 | Apache-2.0 | Copyright 2014-2022 Google LLC |
| github.com/andybalholm/cascadia | v1.3.3 | BSD-2-Clause | Copyright (c) 2011 Andy Balholm. All rights reserved. |
| github.com/coreos/go-oidc/v3 | v3.17.0 | Apache-2.0 | Copyright 2014 CoreOS, Inc |
| github.com/go-chi/chi/v5 | v5.1.0 | MIT | Copyright (c) 2015-present Peter Kieltyka (https://github.com/pkieltyka), Google Inc. |
| github.com/go-jose/go-jose/v4 | v4.1.3 | Apache-2.0 | Copyright 2014-2019 Square Inc.; Copyright 2016 Zbigniew Mandziejewicz; json/ subpackage: Copyright (c) 2012 The Go Authors (BSD-3-Clause) |
| github.com/google/jsonschema-go | v0.4.2 | MIT | Copyright (c) 2025 JSON Schema Go Project Authors |
| github.com/jackc/pgpassfile | v1.0.0 | MIT | Copyright (c) 2019 Jack Christensen |
| github.com/jackc/pgservicefile | v0.0.0-20240606120523-5a60cdf6a761 | MIT | Copyright (c) 2020 Jack Christensen |
| github.com/jackc/pgx/v5 | v5.7.5 | MIT | Copyright (c) 2013-2021 Jack Christensen |
| github.com/jackc/puddle/v2 | v2.2.2 | MIT | Copyright (c) 2018 Jack Christensen |
| github.com/mfridman/interpolate | v0.0.2 | MIT | Copyright (c) 2014-2017 Buildkite Pty Ltd; Copyright (c) 2023 Michael Fridman |
| github.com/modelcontextprotocol/go-sdk | v1.4.0 | Apache-2.0 AND MIT | Copyright 2024-2026 The Go MCP SDK Authors; Copyright (c) 2024-2025 Model Context Protocol a Series of LF Projects, LLC. |
| github.com/pressly/goose/v3 | v3.26.0 | MIT | Original work Copyright (c) 2012 Liam Staskawicz; Modified work Copyright (c) 2016 Vojtech Vitek; Modified work Copyright (c) 2021 Michael Fridman, Vojtech Vitek |
| github.com/robfig/cron/v3 | v3.0.1 | MIT | Copyright (C) 2012 Rob Figueiredo |
| github.com/segmentio/asm | v1.2.0 | MIT | Copyright (c) 2021 Segment |
| github.com/segmentio/encoding | v0.5.3 | MIT | Copyright (c) 2019 Segment.io, Inc. |
| github.com/sethvargo/go-retry | v0.3.0 | Apache-2.0 | Copyright Seth Vargo |
| github.com/yosida95/uritemplate/v3 | v3.0.2 | BSD-3-Clause | Copyright (C) 2016, Kohei YOSHIDA <https://yosida95.com/>. All rights reserved. |
| go.uber.org/multierr | v1.11.0 | MIT | Copyright (c) 2017-2021 Uber Technologies, Inc. |
| golang.org/x/crypto | v0.48.0 | BSD-3-Clause | Copyright 2009 The Go Authors. |
| golang.org/x/net | v0.49.0 | BSD-3-Clause | Copyright 2009 The Go Authors. |
| golang.org/x/oauth2 | v0.35.0 | BSD-3-Clause | Copyright 2009 The Go Authors. |
| golang.org/x/sync | v0.19.0 | BSD-3-Clause | Copyright 2009 The Go Authors. |
| golang.org/x/sys | v0.41.0 | BSD-3-Clause | Copyright 2009 The Go Authors. |
| golang.org/x/term | v0.40.0 | BSD-3-Clause | Copyright 2009 The Go Authors. |
| golang.org/x/text | v0.34.0 | BSD-3-Clause | Copyright 2009 The Go Authors. |
| golang.org/x/time | v0.14.0 | BSD-3-Clause | Copyright 2009 The Go Authors. |

golang.org/x/* modules also ship a PATENTS file granting a patent license from Google; see https://go.dev/PATENTS. github.com/modelcontextprotocol/go-sdk is in transition from MIT to Apache-2.0: new contributions are Apache-2.0 and older ones remain MIT, so both texts apply. Some files in github.com/segmentio/encoding and the json/ package of github.com/go-jose/go-jose/v4 are derived from the Go standard library (BSD-3-Clause, The Go Authors).

## Web UI production bundle

| Component | Version | License | Copyright |
|---|---|---|---|
| @babel/runtime | 7.29.7 | MIT | Copyright (c) 2014-present Sebastian McKenzie and other contributors |
| @babel/runtime-corejs3 | 7.29.7 | MIT | Copyright (c) 2014-present Sebastian McKenzie and other contributors |
| @dagrejs/dagre | 3.1.1 | MIT | Copyright (c) 2012-2014 Chris Pettitt |
| @swagger-api/apidom-ast | 1.11.6 | Apache-2.0 | Copyright 2020-2021 SmartBear Software Inc. |
| @swagger-api/apidom-core | 1.11.6 | Apache-2.0 | Copyright 2020-2021 SmartBear Software Inc. |
| @swagger-api/apidom-error | 1.11.6 | Apache-2.0 | Copyright 2020-2021 SmartBear Software Inc. |
| @swagger-api/apidom-ns-json-schema-2019-09 | 1.11.6 | Apache-2.0 | Copyright 2020-2021 SmartBear Software Inc. |
| @swagger-api/apidom-ns-json-schema-2020-12 | 1.11.6 | Apache-2.0 | Copyright 2020-2021 SmartBear Software Inc. |
| @swagger-api/apidom-ns-json-schema-draft-4 | 1.11.6 | Apache-2.0 | Copyright 2020-2021 SmartBear Software Inc. |
| @swagger-api/apidom-ns-json-schema-draft-6 | 1.11.6 | Apache-2.0 | Copyright 2020-2021 SmartBear Software Inc. |
| @swagger-api/apidom-ns-json-schema-draft-7 | 1.11.6 | Apache-2.0 | Copyright 2020-2021 SmartBear Software Inc. |
| @swagger-api/apidom-ns-openapi-3-0 | 1.11.6 | Apache-2.0 | Copyright 2020-2021 SmartBear Software Inc. |
| @swagger-api/apidom-ns-openapi-3-1 | 1.11.6 | Apache-2.0 | Copyright 2020-2021 SmartBear Software Inc. |
| @swagger-api/apidom-reference | 1.11.6 | Apache-2.0 | Copyright 2020-2021 SmartBear Software Inc. |
| @swaggerexpert/cookie | 2.0.2 | Apache-2.0 | Author: Vladimír Gorej &lt;vladimir.gorej@gmail.com&gt; |
| @swaggerexpert/json-pointer | 2.10.2 | Apache-2.0 | Author: Vladimír Gorej &lt;vladimir.gorej@gmail.com&gt; |
| @tanstack/react-virtual | 3.14.10 | MIT | Copyright (c) 2021-present Tanner Linsley |
| @tanstack/virtual-core | 3.17.8 | MIT | Copyright (c) 2021-present Tanner Linsley |
| @ungap/structured-clone | 1.4.0 | ISC | Copyright (c) 2021, Andrea Giammarchi, @WebReflection |
| @xyflow/react | 12.12.0 | MIT | Copyright (c) 2019-2025 webkid GmbH |
| @xyflow/system | 0.0.83 | MIT | Copyright (c) 2019-2025 webkid GmbH |
| apg-lite | 1.0.5 | BSD-2-Clause | Copyright (c) 2023 Lowell D. Thomas |
| autolinker | 3.16.2 | MIT | Copyright (c) 2014 Gregory Jacobs (http://greg-jacobs.com) |
| available-typed-arrays | 1.0.7 | MIT | Copyright (c) 2020 Inspect JS |
| bail | 2.0.2 | MIT | Copyright (c) 2015 Titus Wormer &lt;tituswormer@gmail.com&gt; |
| base64-js | 1.5.1 | MIT | Copyright (c) 2014 Jameson Little |
| buffer | 6.0.3 | MIT | Copyright (c) Feross Aboukhadijeh, and other contributors. |
| call-bind | 1.0.9 | MIT | Copyright (c) 2020 Jordan Harband |
| call-bind-apply-helpers | 1.0.2 | MIT | Copyright (c) 2024 Jordan Harband |
| call-bound | 1.0.4 | MIT | Copyright (c) 2024 Jordan Harband |
| ccount | 2.0.1 | MIT | Copyright (c) 2015 Titus Wormer &lt;tituswormer@gmail.com&gt; |
| classcat | 5.0.5 | MIT | Copyright © Jorge Bucaran &lt;&lt;https://jorgebucaran.com&gt;&gt; |
| classnames | 2.5.1 | MIT | Copyright (c) 2018 Jed Watson |
| comma-separated-tokens | 2.0.3 | MIT | Copyright (c) 2016 Titus Wormer &lt;tituswormer@gmail.com&gt; |
| copy-to-clipboard | 3.3.3 | MIT | Copyright (c) 2017 sudodoki &lt;smd.deluzion@gmail.com&gt; |
| core-js-pure | 3.50.0 | MIT | Copyright (c) 2013–2025 Denis Pushkarev (zloirock.ru); Copyright (c) 2025–2026 CoreJS Company (core-js.io) |
| css.escape | 1.5.1 | MIT | Copyright Mathias Bynens &lt;https://mathiasbynens.be/&gt; |
| d3-color | 3.1.0 | ISC | Copyright 2010-2022 Mike Bostock |
| d3-dispatch | 3.0.1 | ISC | Copyright 2010-2021 Mike Bostock |
| d3-drag | 3.0.0 | ISC | Copyright 2010-2021 Mike Bostock |
| d3-ease | 3.0.1 | BSD-3-Clause | Copyright 2010-2021 Mike Bostock; Copyright 2001 Robert Penner |
| d3-interpolate | 3.0.1 | ISC | Copyright 2010-2021 Mike Bostock |
| d3-selection | 3.0.0 | ISC | Copyright 2010-2021 Mike Bostock |
| d3-timer | 3.0.1 | ISC | Copyright 2010-2021 Mike Bostock |
| d3-transition | 3.0.1 | ISC | Copyright 2010-2021 Mike Bostock |
| d3-zoom | 3.0.0 | ISC | Copyright 2010-2021 Mike Bostock |
| decode-named-character-reference | 1.3.0 | MIT | Copyright (c) Titus Wormer &lt;tituswormer@gmail.com&gt; |
| deepmerge | 4.3.1 | MIT | Copyright (c) 2012 James Halliday, Josh Duff, and other contributors |
| define-data-property | 1.1.4 | MIT | Copyright (c) 2023 Jordan Harband |
| devlop | 1.1.0 | MIT | Copyright (c) 2023 Titus Wormer &lt;tituswormer@gmail.com&gt; |
| dompurify | 3.4.16 | (MPL-2.0 OR Apache-2.0) - used under Apache-2.0 | Copyright Cure53 and other contributors |
| drange | 1.1.1 | MIT | Copyright (c) 2014 David Tudury |
| dunder-proto | 1.0.1 | MIT | Copyright (c) 2024 ECMAScript Shims |
| es-define-property | 1.0.1 | MIT | Copyright (c) 2024 Jordan Harband |
| es-errors | 1.3.0 | MIT | Copyright (c) 2024 Jordan Harband |
| es-object-atoms | 1.1.2 | MIT | Copyright (c) 2024 Jordan Harband |
| escape-string-regexp | 5.0.0 | MIT | Copyright (c) Sindre Sorhus &lt;sindresorhus@gmail.com&gt; (https://sindresorhus.com) |
| estree-util-is-identifier-name | 3.0.0 | MIT | Copyright (c) 2020 Titus Wormer &lt;tituswormer@gmail.com&gt; |
| extend | 3.0.2 | MIT | Copyright (c) 2014 Stefan Thomas |
| fast-json-patch | 3.1.1 | MIT | Copyright (c) 2013, 2014, 2020 Joachim Wester |
| fault | 1.0.4 | MIT | Copyright (c) 2015 Titus Wormer &lt;tituswormer@gmail.com&gt; |
| for-each | 0.3.5 | MIT | Copyright (c) 2012 Raynos. |
| format | 0.2.2 | MIT | Author: Sami Samhuri &lt;sami@samhuri.net&gt; |
| function-bind | 1.1.2 | MIT | Copyright (c) 2013 Raynos. |
| get-intrinsic | 1.3.0 | MIT | Copyright (c) 2020 Jordan Harband |
| get-proto | 1.0.1 | MIT | Copyright (c) 2025 Jordan Harband |
| gopd | 1.2.0 | MIT | Copyright (c) 2022 Jordan Harband |
| has-property-descriptors | 1.0.2 | MIT | Copyright (c) 2022 Inspect JS |
| has-symbols | 1.1.0 | MIT | Copyright (c) 2016 Jordan Harband |
| has-tostringtag | 1.0.2 | MIT | Copyright (c) 2021 Inspect JS |
| hasown | 2.0.4 | MIT | Copyright (c) Jordan Harband and contributors |
| hast-util-to-jsx-runtime | 2.3.6 | MIT | Copyright (c) Titus Wormer &lt;tituswormer@gmail.com&gt; |
| hast-util-whitespace | 3.0.0 | MIT | Copyright (c) 2016 Titus Wormer &lt;tituswormer@gmail.com&gt; |
| highlight.js | 10.7.3 | BSD-3-Clause | Copyright (c) 2006, Ivan Sagalaev. |
| html-url-attributes | 3.0.1 | MIT | Copyright (c) Titus Wormer |
| ieee754 | 1.2.1 | BSD-3-Clause | Copyright 2008 Fair Oaks Labs, Inc. |
| immutable | 5.1.9 | MIT | Copyright (c) 2014-present, Lee Byron and other contributors. |
| inline-style-parser | 0.2.7 | MIT | Copyright (c) 2012 TJ Holowaychuk &lt;tj@vision-media.ca&gt; |
| is-callable | 1.2.7 | MIT | Copyright (c) 2015 Jordan Harband |
| is-plain-obj | 4.1.0 | MIT | Copyright (c) Sindre Sorhus &lt;sindresorhus@gmail.com&gt; (https://sindresorhus.com) |
| is-typed-array | 1.1.15 | MIT | Copyright (c) 2015 Jordan Harband |
| isarray | 2.0.5 | MIT | Copyright (c) 2013 Julian Gruber &lt;julian@juliangruber.com&gt; |
| js-file-download | 0.4.12 | MIT | Copyright 2017 Kenneth Jiang |
| js-yaml | 4.3.2 | MIT | Copyright (C) 2011-2015 by Vitaly Puzrin |
| lodash | 4.18.1 | MIT | Copyright OpenJS Foundation and other contributors &lt;https://openjsf.org/&gt;; Based on Underscore.js, copyright Jeremy Ashkenas,; Copyright and related rights for sample code are waived via CC0. Sample |
| lodash.debounce | 4.0.8 | MIT | Copyright jQuery Foundation and other contributors &lt;https://jquery.org/&gt;; Based on Underscore.js, copyright Jeremy Ashkenas,; Copyright and related rights for sample code are waived via CC0. Sample |
| longest-streak | 3.1.0 | MIT | Copyright (c) 2015 Titus Wormer &lt;mailto:tituswormer@gmail.com&gt; |
| lowlight | 1.20.0 | MIT | Copyright (c) 2016 Titus Wormer &lt;tituswormer@gmail.com&gt; |
| lucide-react | 1.51.0 | ISC (some icons MIT, derived from Feather) | Copyright (c) 2026 Lucide Icons and Contributors; Feather-derived icons Copyright (c) 2013-present Cole Bemis |
| markdown-table | 3.0.4 | MIT | Copyright (c) Titus Wormer &lt;tituswormer@gmail.com&gt; |
| math-intrinsics | 1.1.0 | MIT | Copyright (c) 2024 ECMAScript Shims |
| mdast-util-find-and-replace | 3.0.2 | MIT | Copyright (c) Titus Wormer &lt;tituswormer@gmail.com&gt; |
| mdast-util-from-markdown | 2.1.0 | MIT | Copyright (c) Titus Wormer &lt;tituswormer@gmail.com&gt; |
| mdast-util-gfm | 3.1.0 | MIT | Copyright (c) Titus Wormer &lt;tituswormer@gmail.com&gt; |
| mdast-util-gfm-autolink-literal | 2.0.1 | MIT | Copyright (c) 2020 Titus Wormer &lt;tituswormer@gmail.com&gt; |
| mdast-util-gfm-footnote | 2.1.0 | MIT | Copyright (c) Titus Wormer &lt;tituswormer@gmail.com&gt; |
| mdast-util-gfm-strikethrough | 2.0.1 | MIT | Copyright (c) Titus Wormer &lt;tituswormer@gmail.com&gt; |
| mdast-util-gfm-table | 2.0.0 | MIT | Copyright (c) 2020 Titus Wormer &lt;tituswormer@gmail.com&gt; |
| mdast-util-gfm-task-list-item | 2.0.0 | MIT | Copyright (c) 2020 Titus Wormer &lt;tituswormer@gmail.com&gt; |
| mdast-util-phrasing | 4.1.0 | MIT | Copyright (c) 2017 Titus Wormer &lt;tituswormer@gmail.com&gt;; Copyright (c) 2017 Victor Felder &lt;victor@draft.li&gt; |
| mdast-util-to-hast | 13.2.1 | MIT | Copyright (c) 2016 Titus Wormer &lt;tituswormer@gmail.com&gt; |
| mdast-util-to-markdown | 2.2.0 | MIT | Copyright (c) Titus Wormer &lt;tituswormer@gmail.com&gt; |
| mdast-util-to-string | 4.0.0 | MIT | Copyright (c) 2015 Titus Wormer &lt;tituswormer@gmail.com&gt; |
| micromark | 4.0.3 | MIT | Copyright (c) Titus Wormer &lt;tituswormer@gmail.com&gt; |
| micromark-core-commonmark | 2.0.4 | MIT | Copyright (c) Titus Wormer &lt;tituswormer@gmail.com&gt; |
| micromark-extension-gfm | 3.0.0 | MIT | Copyright (c) 2020 Titus Wormer &lt;tituswormer@gmail.com&gt; |
| micromark-extension-gfm-autolink-literal | 2.1.0 | MIT | Copyright (c) 2020 Titus Wormer &lt;tituswormer@gmail.com&gt; |
| micromark-extension-gfm-footnote | 2.1.0 | MIT | Copyright (c) 2021 Titus Wormer &lt;tituswormer@gmail.com&gt; |
| micromark-extension-gfm-strikethrough | 2.1.0 | MIT | Copyright (c) 2020 Titus Wormer &lt;tituswormer@gmail.com&gt; |
| micromark-extension-gfm-table | 2.1.2 | MIT | Copyright (c) Titus Wormer &lt;tituswormer@gmail.com&gt; |
| micromark-extension-gfm-task-list-item | 2.1.0 | MIT | Copyright (c) 2020 Titus Wormer &lt;tituswormer@gmail.com&gt; |
| micromark-factory-destination | 2.0.1 | MIT | Copyright (c) Titus Wormer &lt;tituswormer@gmail.com&gt; |
| micromark-factory-label | 2.0.1 | MIT | Copyright (c) Titus Wormer &lt;tituswormer@gmail.com&gt; |
| micromark-factory-space | 2.1.0 | MIT | Copyright (c) Titus Wormer &lt;tituswormer@gmail.com&gt; |
| micromark-factory-title | 2.0.1 | MIT | Copyright (c) Titus Wormer &lt;tituswormer@gmail.com&gt; |
| micromark-factory-whitespace | 2.0.1 | MIT | Copyright (c) Titus Wormer &lt;tituswormer@gmail.com&gt; |
| micromark-util-character | 2.1.1 | MIT | Copyright (c) Titus Wormer &lt;tituswormer@gmail.com&gt; |
| micromark-util-chunked | 2.0.1 | MIT | Copyright (c) Titus Wormer &lt;tituswormer@gmail.com&gt; |
| micromark-util-classify-character | 2.0.1 | MIT | Copyright (c) Titus Wormer &lt;tituswormer@gmail.com&gt; |
| micromark-util-combine-extensions | 2.0.1 | MIT | Copyright (c) Titus Wormer &lt;tituswormer@gmail.com&gt; |
| micromark-util-decode-numeric-character-reference | 2.0.2 | MIT | Copyright (c) Titus Wormer &lt;tituswormer@gmail.com&gt; |
| micromark-util-decode-string | 2.0.1 | MIT | Copyright (c) Titus Wormer &lt;tituswormer@gmail.com&gt; |
| micromark-util-edit-map | 1.0.0 | MIT | Copyright (c) Titus Wormer &lt;tituswormer@gmail.com&gt; |
| micromark-util-html-tag-name | 2.0.1 | MIT | Copyright (c) Titus Wormer &lt;tituswormer@gmail.com&gt; |
| micromark-util-normalize-identifier | 2.0.1 | MIT | Copyright (c) Titus Wormer &lt;tituswormer@gmail.com&gt; |
| micromark-util-resolve-all | 2.0.1 | MIT | Copyright (c) Titus Wormer &lt;tituswormer@gmail.com&gt; |
| micromark-util-sanitize-uri | 2.0.1 | MIT | Copyright (c) Titus Wormer &lt;tituswormer@gmail.com&gt; |
| micromark-util-subtokenize | 2.1.0 | MIT | Copyright (c) Titus Wormer &lt;tituswormer@gmail.com&gt; |
| minim | 0.23.8 | MIT | Copyright (c) 2014 Stephen Mizell |
| neotraverse | 1.0.1 | MIT | Copyright (c) Puru Vijay |
| openapi-path-templating | 2.2.1 | Apache-2.0 | Author: Vladimír Gorej &lt;vladimir.gorej@gmail.com&gt; |
| openapi-server-url-templating | 1.3.0 | Apache-2.0 | Author: Vladimír Gorej &lt;vladimir.gorej@gmail.com&gt; |
| possible-typed-array-names | 1.1.0 | MIT | Copyright (c) 2024 Jordan Harband |
| prop-types | 15.8.1 | MIT | Copyright (c) 2013-present, Facebook, Inc. |
| property-information | 7.2.0 | MIT | Copyright (c) Titus Wormer &lt;mailto:tituswormer@gmail.com&gt; |
| querystringify | 2.2.0 | MIT | Copyright (c) 2015 Unshift.io, Arnout Kazemier,  the Contributors. |
| ramda | 0.30.1 | MIT | Copyright (c) 2013-2024 Scott Sauyet and Michael Hurley |
| ramda-adjunct | 5.1.0 | BSD-3-Clause | Copyright 2017-2019 Vladimír Gorej and the Ramda Adjunct contributors |
| randexp | 0.5.3 | MIT | Copyright (C) 2011 by fent |
| react | 19.3.0 | MIT | Copyright (c) Meta Platforms, Inc. and affiliates. |
| react-copy-to-clipboard | 5.1.1 | MIT | Copyright (c) 2016 Nik Butenko |
| react-debounce-input | 3.3.0 | MIT | Copyright (c) 2016 Nik Butenko |
| react-dom | 19.3.0 | MIT | Copyright (c) Meta Platforms, Inc. and affiliates. |
| react-immutable-proptypes | 2.2.0 | MIT | Copyright (c) 2015 James Burnett |
| react-immutable-pure-component | 2.2.2 | MIT | Copyright (c) 2017 Piotr Tomasz Monarski |
| react-markdown | 10.1.0 | MIT | Copyright (c) Espen Hovlandsdal |
| react-redux | 9.3.0 | MIT | Copyright (c) 2015-present Dan Abramov |
| react-router | 7.18.4 | MIT | Copyright (c) React Training LLC 2015-2019; Copyright (c) Remix Software Inc. 2020-2021; Copyright (c) Shopify Inc. 2022-2023 |
| react-syntax-highlighter | 16.1.1 | MIT | Copyright (c) 2019 Conor Hastings |
| redux | 5.0.1 | MIT | Copyright (c) 2015-present Dan Abramov |
| remark-gfm | 4.0.1 | MIT | Copyright (c) Titus Wormer &lt;tituswormer@gmail.com&gt; |
| remark-parse | 11.0.0 | MIT | Copyright (c) 2014 Titus Wormer &lt;tituswormer@gmail.com&gt; |
| remark-rehype | 11.1.2 | MIT | Copyright (c) Titus Wormer &lt;tituswormer@gmail.com&gt; |
| remarkable | 2.0.1 | MIT | Copyright (c) 2014-2016, Jon Schlinkert; Copyright (c) 2014 Jon Schlinkert, Vitaly Puzrin. |
| repeat-string | 1.6.1 | MIT | Copyright (c) 2014-2016, Jon Schlinkert. |
| requires-port | 1.0.0 | MIT | Copyright (c) 2015 Unshift.io, Arnout Kazemier,  the Contributors. |
| reselect | 5.3.0 | MIT | Copyright (c) 2015-2018 Reselect Contributors |
| ret | 0.2.2 | MIT | Copyright (C) 2011 by fent |
| safe-buffer | 5.2.1 | MIT | Copyright (c) Feross Aboukhadijeh |
| scheduler | 0.28.0 | MIT | Copyright (c) Meta Platforms, Inc. and affiliates. |
| serialize-error | 8.1.0 | MIT | Copyright (c) Sindre Sorhus &lt;sindresorhus@gmail.com&gt; (https://sindresorhus.com) |
| set-function-length | 1.2.2 | MIT | Copyright (c) Jordan Harband and contributors |
| short-unique-id | 5.3.2 | Apache-2.0 | Copyright (c) 2018-2025 Short Unique ID Contributors |
| sonner | 2.0.8 | MIT | Copyright (c) 2023 Emil Kowalski |
| space-separated-tokens | 2.0.2 | MIT | Copyright (c) 2016 Titus Wormer &lt;tituswormer@gmail.com&gt; |
| style-to-js | 1.1.21 | MIT | Copyright (c) 2020 Menglin "Mark" Xu &lt;mark@remarkablemark.org&gt; |
| style-to-object | 1.0.14 | MIT | Copyright (c) 2017 Menglin "Mark" Xu &lt;mark@remarkablemark.org&gt; |
| swagger-client | 3.38.2 | Apache-2.0 | Copyright 2020-2021 SmartBear Software Inc. |
| swagger-ui-react | 5.33.1 | Apache-2.0 | Copyright 2020-2021 SmartBear Software Inc. |
| tailwindcss (generated CSS: preflight/base styles) | 4.3.3 | MIT | Copyright (c) Tailwind Labs, Inc. |
| to-buffer | 1.2.2 | MIT | Copyright (c) 2016 Mathias Buus |
| toggle-selection | 1.0.6 | MIT | Author: sudodoki &lt;smd.deluzion@gmail.com&gt; (sudodoki.name) |
| trim-lines | 3.0.1 | MIT | Copyright (c) 2015 Titus Wormer &lt;mailto:tituswormer@gmail.com&gt; |
| trough | 2.2.0 | MIT | Copyright (c) 2016 Titus Wormer &lt;tituswormer@gmail.com&gt; |
| ts-mixer | 6.0.4 | MIT | Copyright (c) 2024 Tanner Nielsen |
| tslib | 2.8.1 | 0BSD | Copyright (c) Microsoft Corporation. |
| typed-array-buffer | 1.0.3 | MIT | Copyright (c) 2023 Jordan Harband |
| unified | 11.0.5 | MIT | Copyright (c) 2015 Titus Wormer &lt;tituswormer@gmail.com&gt; |
| unist-util-is | 6.0.1 | MIT | Copyright (c) 2015 Titus Wormer &lt;tituswormer@gmail.com&gt; |
| unist-util-position | 5.0.0 | MIT | Copyright (c) 2015 Titus Wormer &lt;tituswormer@gmail.com&gt; |
| unist-util-stringify-position | 4.0.0 | MIT | Copyright (c) 2016 Titus Wormer &lt;tituswormer@gmail.com&gt; |
| unist-util-visit | 5.1.0 | MIT | Copyright (c) 2015 Titus Wormer &lt;tituswormer@gmail.com&gt; |
| unist-util-visit-parents | 6.0.2 | MIT | Copyright (c) 2016 Titus Wormer &lt;tituswormer@gmail.com&gt; |
| url-parse | 1.5.10 | MIT | Copyright (c) 2015 Unshift.io, Arnout Kazemier,  the Contributors. |
| use-sync-external-store | 1.7.0 | MIT | Copyright (c) Meta Platforms, Inc. and affiliates. |
| vfile | 6.0.3 | MIT | Copyright (c) 2015 Titus Wormer &lt;tituswormer@gmail.com&gt; |
| vfile-message | 4.0.3 | MIT | Copyright (c) Titus Wormer &lt;tituswormer@gmail.com&gt; |
| vite / rolldown (build-injected module-preload and runtime helpers) | 8.3.2 | MIT | Copyright (c) 2019-present, VoidZero Inc. and Vite contributors |
| which-typed-array | 1.1.24 | MIT | Copyright (c) 2015 Jordan Harband |
| xml-but-prettier | 1.0.1 | MIT | Copyright (c) 2015 Jonathan Persson |
| zenscroll | 4.0.2 | Unlicense | Public domain dedication (Unlicense), Gyula Szucs |
| zustand | 4.5.7 | MIT | Copyright (c) 2019 Paul Henschel |

Notes: DOMPurify is dual-licensed (MPL-2.0 OR Apache-2.0); Crew Studio uses it under Apache-2.0. lucide-react is ISC, and the icons it derives from Feather are MIT; its complete license is reproduced below. The packages `format`, `toggle-selection` (MIT) and `short-unique-id` (Apache-2.0) do not ship a LICENSE file; their license is taken from package.json/README.

## Python runner dependencies (not redistributed)

Installed by the operator with `pip`/`uv` from `studio/runner/requirements.txt`; not included in this repository or in any image it builds. Listed are the packages the runner and the root helpers actually need at runtime (crewai with its anthropic, google-genai, file-processing and embeddings extras, plus pydantic, httpx, python-dotenv and beautifulsoup4, and everything they require).

| Package | Version | License |
|---|---|---|
| aiocache | 0.12.3 | BSD-3-Clause |
| aiofiles | 24.1.0 | Apache-2.0 |
| aiohappyeyeballs | 2.7.1 | PSF-2.0 |
| aiohttp | 3.14.3 | Apache-2.0 AND MIT |
| aiosignal | 1.4.0 | Apache-2.0 |
| aiosqlite | 0.21.0 | MIT |
| annotated-doc | 0.0.5 | MIT |
| annotated-types | 0.8.0 | MIT |
| anthropic | 0.73.0 | MIT |
| anyio | 4.15.1 | MIT |
| appdirs | 1.4.4 | MIT |
| attrs | 26.1.0 | MIT |
| av | 13.0.0 | BSD-3-Clause (PyAV); the binary wheel bundles FFmpeg built with libx264/libx265 => GPL-2.0-or-later combined |
| backoff | 2.2.1 | MIT |
| bcrypt | 5.0.0 | Apache-2.0 |
| beautifulsoup4 | 4.15.0 | MIT |
| build | 1.6.1 | MIT |
| cel-python | 0.5.0 | Apache-2.0 |
| certifi | 2026.7.22 | MPL-2.0 |
| cffi | 2.1.1 | MIT-0 |
| charset-normalizer | 3.5.2 | MIT |
| chromadb | 1.1.1 | Apache-2.0 |
| click | 8.5.0 | BSD-3-Clause |
| crewai | 1.15.23 | MIT (stated in package README; no license metadata in wheel) |
| crewai-cli | 1.15.23 | MIT (crewAI monorepo; wheel has no license metadata) |
| crewai-core | 1.15.23 | MIT (crewAI monorepo; wheel has no license metadata) |
| crewai-files | 1.15.23 | MIT (crewAI monorepo; wheel has no license metadata) |
| cryptography | 50.0.2 | Apache-2.0 OR BSD-3-Clause |
| deprecation | 2.1.0 | Apache-2.0 |
| distro | 1.9.0 | Apache-2.0 |
| docstring_parser | 0.18.0 | MIT |
| durationpy | 0.11 | MIT |
| et_xmlfile | 2.0.0 | MIT |
| filelock | 4.0.9 | MIT |
| flatbuffers | 25.12.19 | Apache-2.0 |
| frozenlist | 1.8.0 | Apache-2.0 |
| fsspec | 2026.9.0 | BSD-3-Clause |
| google-auth | 2.59.1 | Apache-2.0 |
| google-genai | 1.65.0 | Apache-2.0 |
| google-re2 | 1.1.20251105 | BSD-3-Clause |
| googleapis-common-protos | 1.75.5 | Apache-2.0 |
| grpcio | 1.84.0 | Apache-2.0 |
| h11 | 0.16.0 | MIT |
| hf-xet | 1.6.0 | Apache-2.0 |
| httpcore | 1.0.9 | BSD-3-Clause |
| httptools | 0.8.0 | MIT |
| httpx | 0.28.1 | BSD-3-Clause |
| httpx-sse | 0.4.3 | MIT |
| huggingface_hub | 1.33.0 | Apache-2.0 |
| idna | 3.20 | BSD-3-Clause |
| importlib_resources | 7.1.0 | Apache-2.0 |
| instructor | 1.15.4 | MIT |
| Jinja2 | 3.1.6 | BSD-3-Clause |
| jiter | 0.14.0 | MIT |
| jmespath | 1.1.0 | MIT |
| json_repair | 0.60.1 | MIT |
| json5 | 0.10.0 | Apache-2.0 |
| jsonref | 1.1.0 | MIT |
| jsonschema | 4.26.0 | MIT |
| jsonschema-specifications | 2025.9.1 | MIT |
| kubernetes | 36.0.3 | Apache-2.0 |
| lance-namespace | 0.13.0 | Apache-2.0 |
| lance-namespace-urllib3-client | 0.13.0 | Apache-2.0 |
| lancedb | 0.30.0 | Apache-2.0 |
| lark | 1.3.1 | MIT |
| linkify-it-py | 2.2.0 | MIT |
| markdown-it-py | 4.2.0 | MIT |
| MarkupSafe | 3.0.4 | BSD-3-Clause |
| mcp | 1.28.1 | MIT |
| mdit-py-plugins | 0.6.1 | MIT |
| mdurl | 0.1.2 | MIT |
| mmh3 | 5.3.1 | MIT |
| multidict | 6.9.1 | Apache-2.0 |
| numpy | 2.5.3 | BSD-3-Clause AND 0BSD AND MIT AND Zlib AND CC0-1.0; wheel bundles OpenBLAS (BSD-3), libgfortran (GPL-3.0+ WITH GCC-exception-3.1), libquadmath (LGPL-2.1+) |
| oauthlib | 4.0.0 | BSD-3-Clause |
| onnxruntime | 1.30.0 | MIT (see its ThirdPartyNotices.txt) |
| openai | 2.54.0 | Apache-2.0 |
| openpyxl | 3.1.5 | MIT |
| opentelemetry-api | 1.45.0 | Apache-2.0 |
| opentelemetry-exporter-http-transport | 0.66b0 | Apache-2.0 |
| opentelemetry-exporter-otlp-common | 0.66b0 | Apache-2.0 |
| opentelemetry-exporter-otlp-proto-common | 1.45.0 | Apache-2.0 |
| opentelemetry-exporter-otlp-proto-grpc | 1.45.0 | Apache-2.0 |
| opentelemetry-exporter-otlp-proto-http | 1.45.0 | Apache-2.0 |
| opentelemetry-proto | 1.45.0 | Apache-2.0 |
| opentelemetry-sdk | 1.45.0 | Apache-2.0 |
| opentelemetry-semantic-conventions | 0.66b0 | Apache-2.0 |
| orjson | 3.12.0 | MPL-2.0 AND (Apache-2.0 OR MIT) |
| overrides | 7.7.0 | Apache-2.0 |
| packaging | 26.3 | Apache-2.0 OR BSD-2-Clause |
| pdfminer.six | 20260107 | MIT |
| pdfplumber | 0.11.10 | MIT |
| pendulum | 3.2.0 | MIT |
| pillow | 12.3.0 | MIT-CMU (HPND); wheel bundles libjpeg/libpng/libtiff/freetype/libwebp/lcms2/openjpeg/harfbuzz etc. under their own permissive licenses |
| platformdirs | 4.12.2 | MIT |
| portalocker | 2.7.0 | BSD-3-Clause |
| posthog | 5.4.0 | MIT |
| propcache | 0.5.4 | Apache-2.0 |
| protobuf | 7.36.2 | BSD-3-Clause |
| pyarrow | 25.0.1 | Apache-2.0 (bundles Arrow C++ third-party code; see NOTICE.txt) |
| pyasn1 | 0.6.4 | BSD-2-Clause |
| pyasn1_modules | 0.4.2 | BSD-2-Clause |
| pybase64 | 1.5.0 | BSD-2-Clause |
| pycparser | 3.0 | BSD-3-Clause |
| pydantic | 2.12.5 | MIT |
| pydantic_core | 2.41.5 | MIT |
| pydantic-settings | 2.15.0 | MIT |
| Pygments | 2.21.0 | BSD-2-Clause |
| PyJWT | 2.15.1 | MIT |
| pypdf | 6.16.2 | BSD-3-Clause |
| pypdfium2 | 5.13.0 | BSD-3-Clause OR Apache-2.0 (bindings); bundled PDFium and deps under BSD-3/Apache-2.0/MIT/ICU/FreeType(FTL)/libjpeg/libpng/zlib |
| PyPika | 0.51.1 | Apache-2.0 |
| pyproject_hooks | 1.3.3 | MIT |
| python-dateutil | 2.9.0.post0 | Apache-2.0 AND BSD-3-Clause |
| python-dotenv | 1.2.4 | BSD-3-Clause |
| python-magic | 0.4.27 | MIT |
| python-multipart | 0.0.32 | Apache-2.0 |
| PyYAML | 6.0.3 | MIT |
| referencing | 0.37.0 | MIT |
| regex | 2026.1.15 | Apache-2.0 AND CNRI-Python |
| requests | 2.34.2 | Apache-2.0 |
| requests-oauthlib | 2.0.0 | ISC |
| rich | 14.3.4 | MIT |
| rpds-py | 2026.6.3 | MIT |
| shellingham | 1.5.4 | ISC |
| six | 1.17.0 | MIT |
| sniffio | 1.3.1 | MIT OR Apache-2.0 |
| soupsieve | 2.10 | MIT |
| sse-starlette | 3.5.0 | BSD-3-Clause |
| starlette | 1.7.0 | BSD-3-Clause |
| tenacity | 9.1.4 | Apache-2.0 |
| textual | 8.2.8 | MIT |
| tiktoken | 0.12.0 | MIT |
| tinytag | 2.2.1 | MIT |
| tokenizers | 0.23.2 | Apache-2.0 |
| tomli | 2.0.2 | MIT |
| tomli_w | 1.1.0 | MIT |
| tqdm | 4.70.1 | MPL-2.0 AND MIT |
| typer | 0.27.2 | MIT |
| typing_extensions | 4.16.0 | PSF-2.0 |
| typing-inspection | 0.4.4 | MIT |
| tzdata | 2026.5 | Apache-2.0 |
| urllib3 | 2.8.0 | MIT |
| uv | 0.11.33 | MIT OR Apache-2.0 |
| uvicorn | 0.54.0 | BSD-3-Clause |
| uvloop | 0.23.0 | MIT OR Apache-2.0 |
| watchfiles | 1.3.0 | MIT |
| websocket-client | 1.9.2 | Apache-2.0 |
| websockets | 16.1.1 | BSD-3-Clause |
| yarl | 1.25.1 | Apache-2.0 |

## Apache NOTICE files

Apache-2.0 section 4(d) requires these NOTICE contents to be reproduced when the components are redistributed.

### github.com/coreos/go-oidc/v3 (Go)

```
CoreOS Project
Copyright 2014 CoreOS, Inc

This product includes software developed at CoreOS, Inc.
(http://www.coreos.com/).
```

### @swagger-api/apidom-ast@1.11.6, @swagger-api/apidom-core@1.11.6, @swagger-api/apidom-error@1.11.6, @swagger-api/apidom-ns-json-schema-2019-09@1.11.6, @swagger-api/apidom-ns-json-schema-2020-12@1.11.6, @swagger-api/apidom-ns-json-schema-draft-4@1.11.6, @swagger-api/apidom-ns-json-schema-draft-6@1.11.6, @swagger-api/apidom-ns-json-schema-draft-7@1.11.6, @swagger-api/apidom-ns-openapi-3-0@1.11.6, @swagger-api/apidom-ns-openapi-3-1@1.11.6, @swagger-api/apidom-reference@1.11.6 (npm)

```
ApiDOM
Copyright 2020 SmartBear Software Inc.
ApiDOM is licensed under Apache 2.0 license.
Copy of the Apache 2.0 license can be found in `LICENSES/Apache-2.0.txt` file.

json-schema-ref-parser
Copyright (c) 2015 James Messinger
File packages/apidom-reference/src/util/url.ts (the file) was originally created under MIT license in https://github.com/APIDevTools/json-schema-ref-parser repository.
The file has been copied into this project and modified. All modifications are licensed under Apache 2.0 License.
Copy of the MIT license can be found in `LICENSES/MIT.txt` file.

graphql-js
Copyright (c) GraphQL Contributors
File packages/apidom-ast/src/traversal/visitor.ts (the file) was originally created under MIT license in https://github.com/graphql/graphql-js repository.
The file has been copied into this project and modified. All modifications are licensed under Apache 2.0 License.
Copy of the MIT license can be found in `LICENSES/MIT.txt` file.

babel-plugin-add-import-extension
Copyright (c) 2019 Karl Prieb
File scripts/babel-plugin-add-import-extension.cjs (the file) was originally created under MIT license in https://codeberg.org/karl/babel-plugin-add-import-extension.
The file has been copied into this project and modified. All modifications are licensed under Apache 2.0 License.
Copy of the MIT license can be found in `LICENSES/MIT.txt` file.

refract-spec
Copyright (c) 2015 refractproject
Specific texts in README.md (the file) were originally created under MIT license in https://github.com/refractproject/refract-spec repository.
Specific texts have been copied into this project and modified. All modifications are licensed under Apache 2.0 License.
Copy of the MIT license can be found in `LICENSES/MIT.txt` file.

api-elements
Copyright (c) 2015 Apiary Inc.
Specific texts in README.md (the file) were originally created under MIT license in https://github.com/refractproject/refract-spec repository.
Specific texts have been copied into this project and modified. All modifications are licensed under Apache 2.0 License.
Copy of the MIT license can be found in `LICENSES/MIT.txt` file.

deepmerge
Copyright (c) 2012 James Halliday, Josh Duff, and other contributors
- File packages/apidom-core/src/deepmerge.ts contains algorithms that we originally created
  in https://github.com/TehShrike/deepmerge/blob/master/index.js to handle deep merging of JavaScript Objects and Arrays.
  These algorithms have been reverse engineered and adapted to support deep merging of ApiDOM structures.
- File packages/apidom-core/test/deepmerge.ts contains tests and fixtures that were originally created
  in https://github.com/TehShrike/deepmerge/blob/master/test/merge.js to test deep merging of JavaScript Objects and Arrays.
  These tests have been adapted to support testing deep merging of ApiDOM structures.
- File packages/apidom-core/README.md contains text fragments that were originally created
  in https://github.com/TehShrike/deepmerge/blob/master/readme.md to document deepmerge library.
  These text fragments have been amended to describe merging of ApiDOM structures.
Copy of the MIT license can be found in `LICENSES/MIT.txt` file.
All modifications are licensed under Apache 2.0 License.

OpenAPI Specification 2.0
Copyright The Linux Foundation
Fragments of the specification text are embedded in packages/apidom-ls/src/config/openapi.
Copy of the Apache 2.0 license can be found in `LICENSES/Apache-2.0.txt` file.

OpenAPI Specification 3.0.x
Copyright The Linux Foundation
Fragments of the specification text are embedded in packages/apidom-ls/src/config/openapi.
Copy of the Apache 2.0 license can be found in `LICENSES/Apache-2.0.txt` file.

OpenAPI Specification 3.1.x
Copyright The Linux Foundation
Fragments of the specification text are embedded in packages/apidom-ls/src/config/openapi.
Copy of the Apache 2.0 license can be found in `LICENSES/Apache-2.0.txt` file.

AsyncAPI Specification 2.x / AsyncAPI Initiative
Copyright The Linux Foundation
Fragments of the specification text are embedded in packages/apidom-ls/src/config/asyncapi.
Copy of the Apache 2.0 license can be found in `LICENSES/Apache-2.0.txt` file.

JSON Schema Specification (Draft 4/5 -> 2020-12)
Copyright (c) 2022 JSON Schema Specification Authors
Fragments of the specification text are embedded in packages/apidom-ls/src/config/common.
Copy of the BSD 3-Clause "New" or "Revised" License can be found in `LICENSES/BSD-3-Clause.txt` file.
Copy of the Academic Free License v3.0 can be found in `LICENSES/AFL-3.0.txt` file.

---

If the SPDX-FileCopyrightText and SPDX-License-Identifier tags are not present in the file,
it is assumed that these tags have following implicit value:

SPDX-FileCopyrightText: Copyright 2020-2021 SmartBear Software Inc.
SPDX-License-Identifier: Apache-2.0
```

### @swaggerexpert/cookie@2.0.2 (npm)

```
@swaggerexpert/cookie
Copyright 2024, Vladimír Gorej
@swaggerexpert/cookie is licensed under Apache 2.0 license.
Copy of the Apache 2.0 license can be found in `LICENSE` file.

cookie
Copyright (c) 2012-2014 Roman Shtylman <shtylman@gmail.com>
Copyright (c) 2015 Douglas Christopher Wilson <doug@somethingdoug.com>
Tests in this project are highly inspired by test-suite of cookie project which
is licensed under MIT License. All modifications are licensed under Apache 2.0 License.
No part of test-suite is being distributes within the npm release artifacts.
Copy of the original MIT license can be found in https://github.com/jshttp/cookie/blob/master/LICENSE.
```

### @swaggerexpert/json-pointer@2.10.2 (npm)

```
@swaggerexpert/json-pointer
Copyright 2025, Vladimír Gorej
@swaggerexpert/json-pointer is licensed under Apache 2.0 license.
Copy of the Apache 2.0 license can be found in `LICENSE` file.

JavaScript Object Notation (JSON) Pointer
Copyright (c) 2013 IETF Trust and the persons identified as the document authors.  All rights reserved.
https://datatracker.ietf.org/doc/html/rfc6901
Majority of ABNF grammar (grammar.bnf) and certain texts within the README.md file
are licensed under Simplified BSD License - https://spdx.org/licenses/BSD-2-Clause.html.
```

### openapi-path-templating@2.2.1 (npm)

```
openapi-path-templating
Copyright 2023, Vladimír Gorej
openapi-path-templating is licensed under Apache 2.0 license.
Copy of the Apache 2.0 license can be found in `LICENSE` file.

OpenAPI Specification 3.1.x
Copyright The Linux Foundation
Fragments of the specification text are embedded in `README.md`.
Definition of `template-expression-param-name` ABNF non-terminal is based on the OpenAPI Specification ABNF.
Copy of the Apache 2.0 license can be found in `LICENSE` file.
```

### openapi-server-url-templating@1.3.0 (npm)

```
openapi-server-url-templating
Copyright 2024, Vladimír Gorej
openapi-server-url-templating is licensed under Apache 2.0 license.
Copy of the Apache 2.0 license can be found in `LICENSE` file.

OpenAPI Specification 3.1.x
Copyright The Linux Foundation
Fragments of the specification text are embedded in `README.md`.
Definition of `server-variable-name` ABNF non-terminal is based on the OpenAPI Specification ABNF.
Copy of the Apache 2.0 license can be found in `LICENSE` file.
```

### swagger-client@3.38.2 (npm)

```
swagger-client
Copyright 2020-2021 SmartBear Software Inc.
```

### swagger-ui-react@5.33.1 (npm)

```
swagger-ui
Copyright 2020-2021 SmartBear Software Inc.
```

## License texts

For MIT, BSD and ISC components, the copyright line for each component is the one given in the tables above; the license text below applies with that line substituted for the placeholder.

### Apache License 2.0 (Apache-2.0)

```
Apache License
                           Version 2.0, January 2004
                        http://www.apache.org/licenses/

   TERMS AND CONDITIONS FOR USE, REPRODUCTION, AND DISTRIBUTION

   1. Definitions.

      "License" shall mean the terms and conditions for use, reproduction,
      and distribution as defined by Sections 1 through 9 of this document.

      "Licensor" shall mean the copyright owner or entity authorized by
      the copyright owner that is granting the License.

      "Legal Entity" shall mean the union of the acting entity and all
      other entities that control, are controlled by, or are under common
      control with that entity. For the purposes of this definition,
      "control" means (i) the power, direct or indirect, to cause the
      direction or management of such entity, whether by contract or
      otherwise, or (ii) ownership of fifty percent (50%) or more of the
      outstanding shares, or (iii) beneficial ownership of such entity.

      "You" (or "Your") shall mean an individual or Legal Entity
      exercising permissions granted by this License.

      "Source" form shall mean the preferred form for making modifications,
      including but not limited to software source code, documentation
      source, and configuration files.

      "Object" form shall mean any form resulting from mechanical
      transformation or translation of a Source form, including but
      not limited to compiled object code, generated documentation,
      and conversions to other media types.

      "Work" shall mean the work of authorship, whether in Source or
      Object form, made available under the License, as indicated by a
      copyright notice that is included in or attached to the work
      (an example is provided in the Appendix below).

      "Derivative Works" shall mean any work, whether in Source or Object
      form, that is based on (or derived from) the Work and for which the
      editorial revisions, annotations, elaborations, or other modifications
      represent, as a whole, an original work of authorship. For the purposes
      of this License, Derivative Works shall not include works that remain
      separable from, or merely link (or bind by name) to the interfaces of,
      the Work and Derivative Works thereof.

      "Contribution" shall mean any work of authorship, including
      the original version of the Work and any modifications or additions
      to that Work or Derivative Works thereof, that is intentionally
      submitted to Licensor for inclusion in the Work by the copyright owner
      or by an individual or Legal Entity authorized to submit on behalf of
      the copyright owner. For the purposes of this definition, "submitted"
      means any form of electronic, verbal, or written communication sent
      to the Licensor or its representatives, including but not limited to
      communication on electronic mailing lists, source code control systems,
      and issue tracking systems that are managed by, or on behalf of, the
      Licensor for the purpose of discussing and improving the Work, but
      excluding communication that is conspicuously marked or otherwise
      designated in writing by the copyright owner as "Not a Contribution."

      "Contributor" shall mean Licensor and any individual or Legal Entity
      on behalf of whom a Contribution has been received by Licensor and
      subsequently incorporated within the Work.

   2. Grant of Copyright License. Subject to the terms and conditions of
      this License, each Contributor hereby grants to You a perpetual,
      worldwide, non-exclusive, no-charge, royalty-free, irrevocable
      copyright license to reproduce, prepare Derivative Works of,
      publicly display, publicly perform, sublicense, and distribute the
      Work and such Derivative Works in Source or Object form.

   3. Grant of Patent License. Subject to the terms and conditions of
      this License, each Contributor hereby grants to You a perpetual,
      worldwide, non-exclusive, no-charge, royalty-free, irrevocable
      (except as stated in this section) patent license to make, have made,
      use, offer to sell, sell, import, and otherwise transfer the Work,
      where such license applies only to those patent claims licensable
      by such Contributor that are necessarily infringed by their
      Contribution(s) alone or by combination of their Contribution(s)
      with the Work to which such Contribution(s) was submitted. If You
      institute patent litigation against any entity (including a
      cross-claim or counterclaim in a lawsuit) alleging that the Work
      or a Contribution incorporated within the Work constitutes direct
      or contributory patent infringement, then any patent licenses
      granted to You under this License for that Work shall terminate
      as of the date such litigation is filed.

   4. Redistribution. You may reproduce and distribute copies of the
      Work or Derivative Works thereof in any medium, with or without
      modifications, and in Source or Object form, provided that You
      meet the following conditions:

      (a) You must give any other recipients of the Work or
          Derivative Works a copy of this License; and

      (b) You must cause any modified files to carry prominent notices
          stating that You changed the files; and

      (c) You must retain, in the Source form of any Derivative Works
          that You distribute, all copyright, patent, trademark, and
          attribution notices from the Source form of the Work,
          excluding those notices that do not pertain to any part of
          the Derivative Works; and

      (d) If the Work includes a "NOTICE" text file as part of its
          distribution, then any Derivative Works that You distribute must
          include a readable copy of the attribution notices contained
          within such NOTICE file, excluding those notices that do not
          pertain to any part of the Derivative Works, in at least one
          of the following places: within a NOTICE text file distributed
          as part of the Derivative Works; within the Source form or
          documentation, if provided along with the Derivative Works; or,
          within a display generated by the Derivative Works, if and
          wherever such third-party notices normally appear. The contents
          of the NOTICE file are for informational purposes only and
          do not modify the License. You may add Your own attribution
          notices within Derivative Works that You distribute, alongside
          or as an addendum to the NOTICE text from the Work, provided
          that such additional attribution notices cannot be construed
          as modifying the License.

      You may add Your own copyright statement to Your modifications and
      may provide additional or different license terms and conditions
      for use, reproduction, or distribution of Your modifications, or
      for any such Derivative Works as a whole, provided Your use,
      reproduction, and distribution of the Work otherwise complies with
      the conditions stated in this License.

   5. Submission of Contributions. Unless You explicitly state otherwise,
      any Contribution intentionally submitted for inclusion in the Work
      by You to the Licensor shall be under the terms and conditions of
      this License, without any additional terms or conditions.
      Notwithstanding the above, nothing herein shall supersede or modify
      the terms of any separate license agreement you may have executed
      with Licensor regarding such Contributions.

   6. Trademarks. This License does not grant permission to use the trade
      names, trademarks, service marks, or product names of the Licensor,
      except as required for reasonable and customary use in describing the
      origin of the Work and reproducing the content of the NOTICE file.

   7. Disclaimer of Warranty. Unless required by applicable law or
      agreed to in writing, Licensor provides the Work (and each
      Contributor provides its Contributions) on an "AS IS" BASIS,
      WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
      implied, including, without limitation, any warranties or conditions
      of TITLE, NON-INFRINGEMENT, MERCHANTABILITY, or FITNESS FOR A
      PARTICULAR PURPOSE. You are solely responsible for determining the
      appropriateness of using or redistributing the Work and assume any
      risks associated with Your exercise of permissions under this License.

   8. Limitation of Liability. In no event and under no legal theory,
      whether in tort (including negligence), contract, or otherwise,
      unless required by applicable law (such as deliberate and grossly
      negligent acts) or agreed to in writing, shall any Contributor be
      liable to You for damages, including any direct, indirect, special,
      incidental, or consequential damages of any character arising as a
      result of this License or out of the use or inability to use the
      Work (including but not limited to damages for loss of goodwill,
      work stoppage, computer failure or malfunction, or any and all
      other commercial damages or losses), even if such Contributor
      has been advised of the possibility of such damages.

   9. Accepting Warranty or Additional Liability. While redistributing
      the Work or Derivative Works thereof, You may choose to offer,
      and charge a fee for, acceptance of support, warranty, indemnity,
      or other liability obligations and/or rights consistent with this
      License. However, in accepting such obligations, You may act only
      on Your own behalf and on Your sole responsibility, not on behalf
      of any other Contributor, and only if You agree to indemnify,
      defend, and hold each Contributor harmless for any liability
      incurred by, or claims asserted against, such Contributor by reason
      of your accepting any such warranty or additional liability.

   END OF TERMS AND CONDITIONS

   APPENDIX: How to apply the Apache License to your work.

      To apply the Apache License to your work, attach the following
      boilerplate notice, with the fields enclosed by brackets "{}"
      replaced with your own identifying information. (Don't include
      the brackets!)  The text should be enclosed in the appropriate
      comment syntax for the file format. We also recommend that a
      file or class name and description of purpose be included on the
      same "printed page" as the copyright notice for easier
      identification within third-party archives.

   Copyright {yyyy} {name of copyright owner}

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
```

### MIT License (MIT)

```
MIT License

Copyright (c) <year> <copyright holders>

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

### BSD 2-Clause License (BSD-2-Clause)

```
BSD 2-Clause License

Copyright (c) <year>, <copyright holder>

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:

1. Redistributions of source code must retain the above copyright notice, this
   list of conditions and the following disclaimer.

2. Redistributions in binary form must reproduce the above copyright notice,
   this list of conditions and the following disclaimer in the documentation
   and/or other materials provided with the distribution.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE
FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR
SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER
CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY,
OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```

### BSD 3-Clause License (BSD-3-Clause)

```
BSD 3-Clause License

Copyright (c) <year>, <copyright holder>

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:

1. Redistributions of source code must retain the above copyright notice, this
   list of conditions and the following disclaimer.

2. Redistributions in binary form must reproduce the above copyright notice,
   this list of conditions and the following disclaimer in the documentation
   and/or other materials provided with the distribution.

3. Neither the name of the copyright holder nor the names of its
   contributors may be used to endorse or promote products derived from
   this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE
FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR
SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER
CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY,
OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```

### ISC License (ISC)

```
ISC License

Copyright (c) <year>, <copyright holder>

Permission to use, copy, modify, and/or distribute this software for any
purpose with or without fee is hereby granted, provided that the above
copyright notice and this permission notice appear in all copies.

THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN
ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF
OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.
```

### BSD Zero Clause License (0BSD)

```
BSD Zero Clause License

Copyright (c) <year> <copyright holder>

Permission to use, copy, modify, and/or distribute this software for any
purpose with or without fee is hereby granted.

THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES WITH
REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF MERCHANTABILITY
AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR ANY SPECIAL, DIRECT,
INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES WHATSOEVER RESULTING FROM
LOSS OF USE, DATA OR PROFITS, WHETHER IN AN ACTION OF CONTRACT, NEGLIGENCE OR
OTHER TORTIOUS ACTION, ARISING OUT OF OR IN CONNECTION WITH THE USE OR
PERFORMANCE OF THIS SOFTWARE.
```

### The Unlicense (Unlicense)

```
This is free and unencumbered software released into the public domain.

Anyone is free to copy, modify, publish, use, compile, sell, or
distribute this software, either in source code form or as a compiled
binary, for any purpose, commercial or non-commercial, and by any
means.

In jurisdictions that recognize copyright laws, the author or authors
of this software dedicate any and all copyright interest in the
software to the public domain. We make this dedication for the benefit
of the public at large and to the detriment of our heirs and
successors. We intend this dedication to be an overt act of
relinquishment in perpetuity of all present and future rights to this
software under copyright law.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT.
IN NO EVENT SHALL THE AUTHORS BE LIABLE FOR ANY CLAIM, DAMAGES OR
OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE,
ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR
OTHER DEALINGS IN THE SOFTWARE.

For more information, please refer to <https://unlicense.org/>
```

### lucide-react license (ISC + MIT for Feather-derived icons)

```
ISC License

Copyright (c) 2026 Lucide Icons and Contributors

Permission to use, copy, modify, and/or distribute this software for any
purpose with or without fee is hereby granted, provided that the above
copyright notice and this permission notice appear in all copies.

THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN
ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF
OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.

---

The following Lucide icons are derived from the Feather project:

airplay, alert-circle, alert-octagon, alert-triangle, aperture, arrow-down-circle, arrow-down-left, arrow-down-right, arrow-down, arrow-left-circle, arrow-left, arrow-right-circle, arrow-right, arrow-up-circle, arrow-up-left, arrow-up-right, arrow-up, at-sign, calendar, cast, check, chevron-down, chevron-left, chevron-right, chevron-up, chevrons-down, chevrons-left, chevrons-right, chevrons-up, circle, clipboard, clock, code, columns, command, compass, corner-down-left, corner-down-right, corner-left-down, corner-left-up, corner-right-down, corner-right-up, corner-up-left, corner-up-right, crosshair, database, divide-circle, divide-square, dollar-sign, download, external-link, feather, frown, hash, headphones, help-circle, info, italic, key, layout, life-buoy, link-2, link, loader, lock, log-in, log-out, maximize, meh, minimize, minimize-2, minus-circle, minus-square, minus, monitor, moon, more-horizontal, more-vertical, move, music, navigation-2, navigation, octagon, pause-circle, percent, plus-circle, plus-square, plus, power, radio, rss, search, server, share, shopping-bag, sidebar, smartphone, smile, square, table-2, tablet, target, terminal, trash-2, trash, triangle, tv, type, upload, x-circle, x-octagon, x-square, x, zoom-in, zoom-out

The MIT License (MIT) (for the icons listed above)

Copyright (c) 2013-present Cole Bemis

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```
