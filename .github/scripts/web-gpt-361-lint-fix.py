from pathlib import Path

path = Path("glm-worker/internal/runner/validation_observation.go")
text = path.read_text()
anchor = "type validationSegmentScanner struct {\n"
if text.count(anchor) != 1:
    raise SystemExit("validation scanner anchor mismatch")
text = text.replace(anchor, 'const validationTypeScriptSuite = "tsc"\n\n' + anchor, 1)
text = text.replace('case "tsc":\n\t\treturn "tsc"', 'case validationTypeScriptSuite:\n\t\treturn validationTypeScriptSuite', 1)
text = text.replace('filepath.Base(words[index+1]) == "tsc"', 'filepath.Base(words[index+1]) == validationTypeScriptSuite', 1)
text = text.replace('\t\t\treturn "tsc"\n', '\t\t\treturn validationTypeScriptSuite\n', 1)
path.write_text(text)
