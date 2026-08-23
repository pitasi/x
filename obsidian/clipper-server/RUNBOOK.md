# Runbook: Add a website requiring browser setup

## 1. Prepare

```bash
cd clipper-server/server
npx playwright install chromium
export URL='https://example.com/page'
export STATE='../clipper-state.json'
export NEXT='../clipper-state.next.json'
```

## 2. Capture browser state

First website:

```bash
npx playwright codegen --save-storage="$NEXT" "$URL"
```

Additional website:

```bash
npx playwright codegen --load-storage="$STATE" --save-storage="$NEXT" "$URL"
```

In the opened browser:

- Complete consent, login, or other required steps.
- Visit every required host and regional domain.
- Confirm the target page loads.
- Close the browser.

## 3. Verify and install state

```bash
node -e 'const s=JSON.parse(require("fs").readFileSync(process.argv[1])); console.log([...new Set(s.cookies.map(c=>c.domain))].sort())' "$NEXT"
mv "$NEXT" "$STATE"
chmod 600 "$STATE"
```

## 4. Update the template

- Test the template in Obsidian Web Clipper.
- Export all Web Clipper settings.
- Replace `obsidian-web-clipper-settings.json`.

## 5. Deploy

```bash
scp "$STATE" vps:/opt/clipper/clipper-state.json
scp ../../obsidian-web-clipper-settings.json vps:/opt/clipper/obsidian-web-clipper-settings.json
```

Container configuration:

```text
CLIPPER_STORAGE_STATE=/run/secrets/clipper-state.json
BROWSER_DATA_DIR=/data
```

```text
/opt/clipper/clipper-state.json:/run/secrets/clipper-state.json:ro
/opt/clipper/obsidian-web-clipper-settings.json:/settings.json:ro
clipper-data:/data
```

Restart `clipper-server`.

## 6. Verify

```bash
export CLIPPER_URL='https://clipper.anto.pt'
curl -fsS --json "{\"url\":\"$URL\"}" "$CLIPPER_URL/clip" | python3 -m json.tool
```

Check:

- Correct template
- Non-empty note name
- Expected DOM-derived fields
- Expected URL-derived fields
