#!/usr/bin/env python3
"""Build, check, sign, verify and decode Apple Shortcuts.

    shortcut.py build  SPEC.json  OUT.plist      compile a readable spec into a shortcut plist (XML)
    shortcut.py check  IN.plist                  structural checks: references, menus, identifiers
    shortcut.py sign   IN.plist   OUT.shortcut   sign for anyone to import (macOS, signed in to iCloud)
    shortcut.py verify IN.shortcut [--against IN.plist]   check the signature, unpack, compare
    shortcut.py decode IN.shortcut|IN.plist OUT.plist     unpack a signed or unsigned shortcut to XML
    shortcut.py spec   IN.shortcut|IN.plist OUT.json      the reverse of build: a shortcut back to a spec

A spec is JSON:

    {
      "name": "Log a habit",                      # for messages only; the file name names the shortcut
      "icon": {"glyph": 62337, "color": 946986751},   # optional
      "import_questions": [                       # optional: asked when someone imports it
        {"action": "send", "param": "WFURL", "text": "Your server's address", "default": "http://"}
      ],
      "actions": [ ...steps... ]
    }

Each step is {"action": "<identifier>", "id": "<name>", "params": {...}}. The identifier may
drop the "is.workflow.actions." prefix. "id" names the step so later steps can use its output.
Inside params:

  "{{id}}"                  the step's output. In an input parameter (WFInput, Split Text's
                            text, If's input) it's passed as the variable itself; in any
                            other field it's text holding the variable, which is what text
                            fields need: a bare variable there shows up empty on the phone
  "text {{id}} more"        a string with outputs inside it (a token string)
  "{{id:Output Name}}"      the same, naming the output when the action isn't in OUTPUT_NAMES
  "{{ask}}" / "{{clipboard}}" / "{{date}}" / "{{input}}"   Ask Each Time, Clipboard, Current Date, Shortcut Input
  {"$var": "Name"}          a named variable (set with setvariable)
  {"$text": "..."}          force a token string even when it is a lone "{{id}}"
  {"$dict": {"k": "v"}}     a dictionary field (headers, JSON bodies, Dictionary action); values may use {{id}}
  {"$raw": ...}             passed through untouched

Control flow, as single steps holding their branches:

  {"menu": {"prompt": "When?", "items": {"Now": [...], "Earlier": [...]}}, "id": "when"}
      -> Choose from Menu; "{{when}}" is the Menu Result (the last output of the branch taken)
  {"if": {"input": "{{x}}", "condition": "is", "value": "Yes"}, "then": [...], "else": [...], "id": "r"}
  {"repeat": 3, "do": [...], "id": "r"}            {"repeat_each": "{{list}}", "do": [...], "id": "r"}
"""
import json
import os
import plistlib
import re
import shutil
import struct
import subprocess
import sys
import tempfile
import uuid

PREFIX = "is.workflow.actions."
OBJ = "￼"  # the object replacement character a token occupies in a string

# What each action's output is called in the Shortcuts editor. A reference needs the name;
# the ones marked * come from real exports, the rest from the Shortcuts UI.
OUTPUT_NAMES = {
    "downloadurl": "Contents of URL",          # *
    "detect.text": "Text",                     # *
    "text.split": "Split Text",                # *
    "choosefromlist": "Selected Item",         # *
    "gettext": "Text",                         # *
    "ask": "Provided Input",                   # *
    "detect.date": "Dates",                    # *
    "format.date": "Formatted Date",           # *
    "choosefrommenu": "Menu Result",           # *
    "url": "URL",
    "getclipboard": "Clipboard",
    "number": "Number",
    "math": "Calculation Result",
    "dictionary": "Dictionary",
    "getvalueforkey": "Dictionary Value",
    "list": "List",
    "date": "Date",
    "adjustdate": "Adjusted Date",
    "gettimebetweendates": "Time Between Dates",
    "conditional": "If Result",
    "repeat.count": "Repeat Results",
    "repeat.each": "Repeat Results",
    "count": "Count",
    "getitemfromlist": "Item from List",
    "text.combine": "Combined Text",
    "text.replace": "Updated Text",
    "text.changecase": "Updated Text",
    "detect.number": "Numbers",
    "detect.dictionary": "Dictionary",
    "getvariable": "Variable",
    "getcurrentlocation": "Current Location",
    "weather.currentconditions": "Weather Conditions",
    "runworkflow": "Shortcut Result",
}

# Identifiers known to exist. A step outside this list still builds; check warns, so a typo
# shows up before the phone silently drops the step.
KNOWN = set(OUTPUT_NAMES) | {
    "speaktext", "showresult", "notification", "alert", "comment", "openurl", "setclipboard",
    "setvariable", "appendvariable", "nothing", "exit", "delay", "vibrate", "previewdocument",
    "sendmessage", "sendemail", "openapp", "getmyworkflows", "searchweb", "getwebpagecontents",
    "file.createfolder", "documentpicker.save", "documentpicker.open", "takephoto", "selectphoto",
    "playmusic", "setvolume", "setbrightness", "lowpowermode.set", "flashlight", "dnd.set",
    "properties.contacts", "properties.calendarevents", "addnewevent", "addnewreminder",
    "getupcomingevents", "health.quantity.log", "location.getcurrent", "getdevicedetails",
    "getbatterylevel", "wifi.set", "bluetooth.set", "airdropdocument", "share", "hash",
    "base64encode", "urlencode", "random", "round", "statistics", "format.number",
}

# Parameters that take another step's output as the variable itself, as
# real exports write them (WFInput, Split Text's text). Every other field is
# text, and gets a lone {{id}} as a string holding the variable: given a
# bare variable, a text field (Show Result's, Speak Text's) shows up empty.
INPUT_KEYS = {"WFInput", "text", "WFVariable"}

SPECIAL = {
    "ask": {"Type": "Ask"},
    "clipboard": {"Type": "Clipboard"},
    "date": {"Type": "CurrentDate"},
    "input": {"Type": "ExtensionInput"},
}

REF = re.compile(r"\{\{([^{}:]+)(?::([^{}]+))?\}\}")


# IDs are derived from the spec, not random, so an unchanged spec builds a
# byte-identical plist: diffs show real changes, and an unchanged shortcut
# needn't be signed again.
UUID_NAMESPACE = uuid.UUID("6f1d3a52-7c0e-4d2b-9a55-3e8f5b2c7d10")


class Builder:
    def __init__(self, seed=""):
        self.actions = []
        self.ids = {}  # step id -> (uuid, output name)
        self.seed, self.n = seed, 0

    def new_uuid(self):
        """The next ID: the same for the same spec, build after build."""
        self.n += 1
        return str(uuid.uuid5(UUID_NAMESPACE, f"{self.seed}#{self.n}")).upper()

    # -- values -------------------------------------------------------------
    def ref(self, name, out=None):
        if name in SPECIAL:
            return dict(SPECIAL[name])
        if name not in self.ids:
            raise SystemExit(f"error: '{{{{{name}}}}}' uses a step that isn't defined before it")
        uid, default = self.ids[name]
        out = out or default
        if not out:
            raise SystemExit(f"error: '{name}' has no known output name; write {{{{{name}:Output Name}}}}")
        return {"OutputName": out, "OutputUUID": uid, "Type": "ActionOutput"}

    def token_string(self, s):
        parts, attachments, pos = [], {}, 0
        for m in REF.finditer(s):
            parts.append(s[pos:m.start()])
            at = len("".join(parts).encode("utf-16-le")) // 2  # ranges count UTF-16 units
            attachments[f"{{{at}, 1}}"] = self.ref(m.group(1).strip(), m.group(2))
            parts.append(OBJ)
            pos = m.end()
        parts.append(s[pos:])
        value = {"string": "".join(parts)}
        if attachments:
            value["attachmentsByRange"] = attachments
        return {"Value": value, "WFSerializationType": "WFTextTokenString"}

    def value(self, v, key=""):
        if isinstance(v, str):
            m = REF.fullmatch(v.strip())
            if m and key in INPUT_KEYS:
                return {"Value": self.ref(m.group(1).strip(), m.group(2)), "WFSerializationType": "WFTextTokenAttachment"}
            if m:
                return self.token_string(v)
            if REF.search(v):
                return self.token_string(v)
            return v
        if isinstance(v, dict):
            if "$raw" in v:
                return v["$raw"]
            if "$text" in v:
                return self.token_string(v["$text"])
            if "$var" in v:
                return {"Value": {"Type": "Variable", "VariableName": v["$var"]}, "WFSerializationType": "WFTextTokenAttachment"}
            if "$dict" in v:
                items = [{"WFItemType": 0, "WFKey": self.token_string(str(k)), "WFValue": self.token_string(str(val))}
                         for k, val in v["$dict"].items()]
                return {"Value": {"WFDictionaryFieldValueItems": items}, "WFSerializationType": "WFDictionaryFieldValue"}
            return {k: self.value(x, k) for k, x in v.items()}
        if isinstance(v, list):
            return [self.value(x) for x in v]
        return v

    # -- steps --------------------------------------------------------------
    def add(self, ident, params, step_id=None, out=None):
        ident = full_ident(ident)
        p = dict(params)
        if step_id or "UUID" not in p:
            p.setdefault("UUID", self.new_uuid())
        self.actions.append({"WFWorkflowActionIdentifier": ident, "WFWorkflowActionParameters": p})
        if step_id:
            if step_id in self.ids:
                raise SystemExit(f"error: two steps are named '{step_id}'")
            self.ids[step_id] = (p["UUID"], out or OUTPUT_NAMES.get(ident[len(PREFIX):]))
        return p

    def steps(self, steps):
        for s in steps:
            self.step(s)

    def step(self, s):
        if "menu" in s:
            return self.menu(s)
        if "if" in s:
            return self.conditional(s)
        if "repeat" in s or "repeat_each" in s:
            return self.repeat(s)
        params = self.value(s.get("params", {}))
        if "uuid" in s:  # kept by spec: $raw content elsewhere links to this step by it
            params["UUID"] = s["uuid"]
        self.add(s["action"], params, s.get("id"), s.get("output"))

    def menu(self, s):
        m, group = s["menu"], self.new_uuid()
        items = m["items"]
        self.add("choosefrommenu", {"GroupingIdentifier": group, "WFControlFlowMode": 0,
                                    "WFMenuItems": list(items), "WFMenuPrompt": m.get("prompt", "")})
        self.actions[-1]["WFWorkflowActionParameters"].pop("UUID")
        for title, body in items.items():
            self.actions.append({"WFWorkflowActionIdentifier": PREFIX + "choosefrommenu", "WFWorkflowActionParameters":
                                 {"GroupingIdentifier": group, "WFControlFlowMode": 1, "WFMenuItemTitle": title}})
            self.steps(body)
        self.add("choosefrommenu", end_params(group, s), s.get("id"), "Menu Result")

    # The condition codes are the ones the Shortcuts editor writes (not from a verified export).
    CONDITIONS = {"is": 4, "is not": 5, "has any value": 100, "does not have any value": 101,
                  "contains": 99, "does not contain": 999, "begins with": 8, "ends with": 9,
                  "is greater than": 2, "is less than": 0, "is greater than or equal to": 3,
                  "is less than or equal to": 1}

    def conditional(self, s):
        c, group = s["if"], self.new_uuid()
        cond = c.get("condition", "is")
        if cond not in self.CONDITIONS:
            raise SystemExit(f"error: unknown condition '{cond}'; one of {sorted(self.CONDITIONS)}")
        p = {"GroupingIdentifier": group, "WFControlFlowMode": 0, "WFCondition": self.CONDITIONS[cond],
             # the input as a direct link to the step's output (WFTextTokenAttachment), as
             # the editor writes it (from a phone export): given a text token instead,
             # the phone shows the whole condition blank
             "WFInput": {"Type": "Variable", "Variable": self.value(c["input"], "WFInput")}}
        if "value" in c:
            if isinstance(c["value"], (int, float)):
                p["WFNumberValue"] = c["value"]
            else:
                p["WFConditionalActionString"] = self.value(c["value"])
        self.actions.append({"WFWorkflowActionIdentifier": PREFIX + "conditional", "WFWorkflowActionParameters": p})
        self.steps(s.get("then", []))
        if s.get("else"):
            self.actions.append({"WFWorkflowActionIdentifier": PREFIX + "conditional",
                                 "WFWorkflowActionParameters": {"GroupingIdentifier": group, "WFControlFlowMode": 1}})
            self.steps(s["else"])
        self.add("conditional", end_params(group, s), s.get("id"), "If Result")

    def repeat(self, s):
        group = self.new_uuid()
        if "repeat" in s:
            ident, p = "repeat.count", {"WFRepeatCount": s["repeat"]}
        else:
            ident, p = "repeat.each", {"WFInput": self.value(s["repeat_each"])}
        p.update({"GroupingIdentifier": group, "WFControlFlowMode": 0})
        self.actions.append({"WFWorkflowActionIdentifier": PREFIX + ident, "WFWorkflowActionParameters": p})
        self.steps(s["do"])
        self.add(ident, end_params(group, s), s.get("id"), "Repeat Results")


def full_ident(ident):
    """An action's full identifier: Apple's own are written without their
    "is.workflow.actions." prefix in a spec; another app's (com.example...)
    are written in full and left alone."""
    if ident.startswith(PREFIX) or ident.split(".")[0] in ("com", "org", "net", "io", "dev", "app", "co", "me"):
        return ident
    return PREFIX + ident


def end_params(group, s):
    """A block's closing action, which carries the block's output and so its
    UUID: an explicit "uuid" on the step when the spec gives one."""
    p = {"GroupingIdentifier": group, "WFControlFlowMode": 2}
    if "uuid" in s:
        p["UUID"] = s["uuid"]
    return p


def build(spec_path, out_path):
    spec = json.load(open(spec_path))
    b = Builder(spec.get("name") or os.path.basename(spec_path))
    b.steps(spec["actions"])
    icon = spec.get("icon", {})
    questions = []
    for q in spec.get("import_questions", []):
        target = b.ids.get(q["action"])
        if not target:
            raise SystemExit(f"error: import question for unknown step '{q['action']}'")
        index = next(i for i, a in enumerate(b.actions) if a["WFWorkflowActionParameters"].get("UUID") == target[0])
        questions.append({"ActionIndex": index, "Category": "Parameter", "DefaultValue": q.get("default", ""),
                          "ParameterKey": q["param"], "Text": q["text"]})
    wf = {
        "WFQuickActionSurfaces": [],
        "WFWorkflowActions": b.actions,
        "WFWorkflowClientVersion": "2605.0.5",
        "WFWorkflowHasOutputFallback": False,
        "WFWorkflowHasShortcutInputVariables": False,
        "WFWorkflowIcon": {"WFWorkflowIconGlyphNumber": icon.get("glyph", 59511),
                           "WFWorkflowIconStartColor": icon.get("color", 946986751)},
        "WFWorkflowImportQuestions": questions,
        "WFWorkflowInputContentItemClasses": ["WFStringContentItem", "WFURLContentItem"],
        "WFWorkflowMinimumClientVersion": 900,
        "WFWorkflowMinimumClientVersionString": "900",
        "WFWorkflowOutputContentItemClasses": [],
        "WFWorkflowTypes": spec.get("types", []),
    }
    with open(out_path, "wb") as f:
        plistlib.dump(wf, f, fmt=plistlib.FMT_XML)
    problems = check_workflow(wf)
    print(f"built {out_path}: {len(b.actions)} actions")
    for p in problems:
        print("  " + p)
    return 1 if any(p.startswith("error") for p in problems) else 0


def check_workflow(wf):
    """Return problems: 'error: ...' breaks the shortcut, 'warning: ...' may."""
    out = []
    acts = wf.get("WFWorkflowActions")
    if not isinstance(acts, list) or not acts:
        return ["error: no WFWorkflowActions"]
    defined = set()
    groups = {}
    for i, a in enumerate(acts):
        ident = a.get("WFWorkflowActionIdentifier", "")
        p = a.get("WFWorkflowActionParameters", {})
        short = ident[len(PREFIX):] if ident.startswith(PREFIX) else ident
        if not ident.startswith(PREFIX) and "." not in ident:
            out.append(f"error: action {i}: identifier '{ident}' isn't a full identifier")
        elif ident.startswith(PREFIX) and short not in KNOWN:
            out.append(f"warning: action {i}: '{short}' isn't a known action; a phone drops an unknown one silently")
        # references must point at an earlier action
        for ref_uuid, where in refs(p):
            if ref_uuid not in defined:
                out.append(f"error: action {i} ({short}) uses output {ref_uuid} that no earlier action produces ({where})")
        # token strings: each range must land on an object character
        for s, rng in token_ranges(p):
            units = s.encode("utf-16-le")
            for r in rng:
                at = int(r.strip("{}").split(",")[0])
                if units[at * 2:at * 2 + 2] != OBJ.encode("utf-16-le"):
                    out.append(f"error: action {i} ({short}): attachment {r} isn't on a \\uFFFC in {s!r}")
        g = p.get("GroupingIdentifier")
        if g is not None:
            mode = p.get("WFControlFlowMode")
            stack = groups.setdefault(g, [])
            stack.append(mode)
        if "UUID" in p:
            defined.add(p["UUID"])
    for g, modes in groups.items():
        if not modes or modes[0] != 0 or modes[-1] != 2:
            out.append(f"error: block {g} doesn't start with mode 0 and end with mode 2: {modes}")
    return out


def refs(obj, path=""):
    if isinstance(obj, dict):
        if obj.get("Type") == "ActionOutput" and "OutputUUID" in obj:
            yield obj["OutputUUID"], path or "parameter"
        for k, v in obj.items():
            yield from refs(v, f"{path}.{k}" if path else k)
    elif isinstance(obj, list):
        for i, v in enumerate(obj):
            yield from refs(v, f"{path}[{i}]")


def token_ranges(obj):
    if isinstance(obj, dict):
        if isinstance(obj.get("string"), str) and isinstance(obj.get("attachmentsByRange"), dict):
            yield obj["string"], list(obj["attachmentsByRange"])
        for v in obj.values():
            yield from token_ranges(v)
    elif isinstance(obj, list):
        for v in obj:
            yield from token_ranges(v)


def check(path):
    wf = plistlib.load(open(path, "rb"))
    problems = check_workflow(wf)
    lint = subprocess.run(["plutil", "-lint", path], capture_output=True, text=True)
    if lint.returncode:
        problems.insert(0, "error: " + lint.stdout.strip())
    for p in problems:
        print(p)
    if not problems:
        print(f"ok: {len(wf['WFWorkflowActions'])} actions, every reference resolves, every block closes")
    return 1 if any(p.startswith("error") for p in problems) else 0


def sign(plist_path, out_path):
    if shutil.which("shortcuts") is None:
        raise SystemExit("error: the `shortcuts` command isn't here; signing needs macOS 12 or later")
    if check(plist_path):
        raise SystemExit("error: fix the problems above before signing")
    with tempfile.TemporaryDirectory() as tmp:
        unsigned = os.path.join(tmp, "unsigned.shortcut")  # sign refuses any other extension
        shutil.copy(plist_path, unsigned)
        r = subprocess.run(["shortcuts", "sign", "--mode", "anyone", "--input", unsigned, "--output", out_path],
                           capture_output=True, text=True, timeout=180)
    noise = [l for l in (r.stdout + r.stderr).splitlines() if l and "Unrecognized attribute string flag" not in l]
    if r.returncode or not os.path.exists(out_path):
        raise SystemExit("error: shortcuts sign failed (signed in to iCloud? online?)\n" + "\n".join(noise))
    print(f"signed {out_path}")
    return verify(out_path, plist_path)


def unpack(signed_path, workdir):
    """Check a signed shortcut's signature and return its workflow plist."""
    d = open(signed_path, "rb").read()
    if d[:4] != b"AEA1":
        return plistlib.loads(d), None  # an unsigned shortcut is the plist itself
    n = struct.unpack("<I", d[8:12])[0]
    auth = plistlib.loads(d[12:12 + n])
    chain = auth.get("SigningCertificateChain") or []
    if not chain:
        raise SystemExit("error: signed, but with no certificate chain (a 'people who know me' file?)")
    leaf, pem, aar, out = (os.path.join(workdir, x) for x in ("leaf.der", "leaf.pem", "payload.aar", "x"))
    open(leaf, "wb").write(chain[0])
    subprocess.run(["openssl", "x509", "-inform", "der", "-in", leaf, "-pubkey", "-noout", "-out", pem], check=True)
    r = subprocess.run(["aea", "decrypt", "-i", signed_path, "-o", aar, "-sign-pub", pem], capture_output=True, text=True)
    if r.returncode:
        raise SystemExit("error: the signature doesn't verify: " + r.stderr.strip())
    os.makedirs(out)
    subprocess.run(["aa", "extract", "-i", aar, "-d", out], check=True)
    subj = subprocess.run(["openssl", "x509", "-inform", "der", "-in", leaf, "-noout", "-subject", "-issuer"],
                          capture_output=True, text=True).stdout.strip().replace("\n", "; ")
    return plistlib.load(open(os.path.join(out, "Shortcut.wflow"), "rb")), subj


def strip_volatile(wf):
    wf = json.loads(json.dumps(wf, default=str))
    wf.pop("WFWorkflowClientVersion", None)
    return wf


def verify(signed_path, against=None):
    with tempfile.TemporaryDirectory() as tmp:
        wf, signer = unpack(signed_path, tmp)
    print(f"signature ok: {signer}" if signer else "unsigned: a phone won't import this; sign it first")
    problems = check_workflow(wf)
    for p in problems:
        print("  " + p)
    if against:
        same = strip_volatile(wf) == strip_volatile(plistlib.load(open(against, "rb")))
        print("contents match " + against if same else "contents DIFFER from " + against)
        if not same:
            return 1
    print(f"{len(wf['WFWorkflowActions'])} actions")
    return 1 if any(p.startswith("error") for p in problems) or not signer else 0


def decode(in_path, out_path):
    with tempfile.TemporaryDirectory() as tmp:
        wf, signer = unpack(in_path, tmp)
    with open(out_path, "wb") as f:
        plistlib.dump(wf, f, fmt=plistlib.FMT_XML)
    print(f"wrote {out_path} ({len(wf['WFWorkflowActions'])} actions{', signed: ' + signer if signer else ''})")
    for i, a in enumerate(wf["WFWorkflowActions"]):
        p = a.get("WFWorkflowActionParameters", {})
        keys = ", ".join(k for k in p if k not in ("UUID", "GroupingIdentifier"))
        print(f"  {i:2} {a['WFWorkflowActionIdentifier'].replace(PREFIX, '')}  [{keys}]")
    return 0



# ---- spec: the reverse of build -------------------------------------------

class Unspecable(Exception):
    """A value the spec shorthand can't say; kept as $raw instead."""


class Decompiler:
    """Turns a workflow back into a spec: the reverse of Builder."""

    def __init__(self, wf):
        self.wf = wf
        self.acts = wf.get("WFWorkflowActions", [])
        self.ids = {}       # UUID -> step id
        self.defaults = {}  # UUID -> the action's default output name
        self.keep = set()   # UUIDs that $raw content links to: kept in the spec
        used = {}
        def link(uid, name):
            if uid in self.ids:
                return
            base = re.sub(r"[^a-z0-9]+", "_", (name or "step").lower()).strip("_") or "step"
            n = used.get(base, 0) + 1
            used[base] = n
            self.ids[uid] = base if n == 1 else f"{base}_{n}"
        for uid, name in self.references(self.acts):
            link(uid, name)
        for q in wf.get("WFWorkflowImportQuestions", []):  # a question's step needs an id too
            i = q.get("ActionIndex")
            if isinstance(i, int) and 0 <= i < len(self.acts):
                p = self.acts[i].get("WFWorkflowActionParameters", {})
                if "UUID" in p:
                    link(p["UUID"], q.get("ParameterKey", "step").removeprefix("WF"))
        for a in self.acts:
            p = a.get("WFWorkflowActionParameters", {})
            if "UUID" in p:
                self.defaults[p["UUID"]] = OUTPUT_NAMES.get(a.get("WFWorkflowActionIdentifier", "").removeprefix(PREFIX))

    @staticmethod
    def references(obj):
        """Every (UUID, output name) an action output is used by, in order."""
        if isinstance(obj, dict):
            if obj.get("Type") == "ActionOutput" and "OutputUUID" in obj:
                yield obj["OutputUUID"], obj.get("OutputName")
            for v in obj.values():
                yield from Decompiler.references(v)
        elif isinstance(obj, list):
            for v in obj:
                yield from Decompiler.references(v)

    # -- values: the reverse of Builder.value --------------------------------
    def ref(self, att):
        t = att.get("Type")
        if set(att) - {"Type", "OutputName", "OutputUUID", "VariableName"}:
            raise Unspecable  # aggrandizements (a property of a value) and the like
        if t == "ActionOutput":
            uid, name = att["OutputUUID"], att.get("OutputName")
            if uid not in self.ids:
                raise Unspecable
            sid = self.ids[uid]
            return f"{{{{{sid}}}}}" if name == self.defaults.get(uid) else f"{{{{{sid}:{name}}}}}"
        for word, special in SPECIAL.items():
            if att == special:
                return f"{{{{{word}}}}}"
        raise Unspecable

    def text(self, value):
        """A token string's text, with each variable back as {{id}}."""
        string = value.get("string", "")
        atts = value.get("attachmentsByRange", {})
        units = string.encode("utf-16-le")
        out, pos = [], 0
        for r, att in sorted(atts.items(), key=lambda kv: int(kv[0].strip("{}").split(",")[0])):
            at = int(r.strip("{}").split(",")[0])
            out.append(units[pos * 2:at * 2].decode("utf-16-le"))
            out.append(self.ref(att))
            pos = at + 1
        out.append(units[pos * 2:].decode("utf-16-le"))
        return "".join(out)

    def value(self, v, key=""):
        if isinstance(v, dict):
            kind = v.get("WFSerializationType")
            if kind == "WFTextTokenAttachment":
                val = v.get("Value", {})
                if val.get("Type") == "Variable" and set(val) == {"Type", "VariableName"}:
                    return {"$var": val["VariableName"]}
                r = self.ref(val)
                return r if key in INPUT_KEYS else {"$raw": v}  # build writes a lone ref elsewhere as text
            if kind == "WFTextTokenString":
                t = self.text(v.get("Value", {}))
                # build writes a plain string for text without variables, and a lone
                # {{id}} in an input as the variable itself: say "text" outright there
                if not v.get("Value", {}).get("attachmentsByRange") or (key in INPUT_KEYS and REF.fullmatch(t)):
                    return {"$text": t}
                return t
            if kind == "WFDictionaryFieldValue":
                items = v.get("Value", {}).get("WFDictionaryFieldValueItems", [])
                d = {}
                for it in items:
                    if it.get("WFItemType") != 0 or set(it) != {"WFItemType", "WFKey", "WFValue"}:
                        raise Unspecable
                    k = self.text(it["WFKey"].get("Value", {}))
                    if k in d:
                        raise Unspecable
                    d[k] = self.text(it["WFValue"].get("Value", {}))
                return {"$dict": d}
            if kind is not None:
                raise Unspecable
            return {k: self.value(x, k) for k, x in v.items()}
        if isinstance(v, list):
            return [self.value(x) for x in v]
        if isinstance(v, str) and REF.search(v):
            raise Unspecable  # literal braces would read back as a reference
        return v

    def params(self, p):
        out = {}
        for k, v in p.items():
            if k == "UUID":
                continue
            try:
                out[k] = self.value(v, k)
            except Unspecable:
                out[k] = {"$raw": v}
                self.keep.update(uid for uid, _ in self.references(v))
        return out

    # -- steps: the reverse of Builder.steps ---------------------------------
    def steps(self, acts):
        out, i = [], 0
        while i < len(acts):
            a = acts[i]
            p = a.get("WFWorkflowActionParameters", {})
            ident = a.get("WFWorkflowActionIdentifier", "")
            short = ident.removeprefix(PREFIX)
            if p.get("WFControlFlowMode") == 0 and "GroupingIdentifier" in p:
                end = self.block_end(acts, i)
                block = self.block(acts[i:end + 1])
                if block is not None:
                    out.append(block)
                    i = end + 1
                    continue
            step = {"action": short if ident.startswith(PREFIX) else ident}
            uid = p.get("UUID")
            if uid in self.ids:
                step["id"] = self.ids[uid]
            params = self.params(p)
            if params:
                step["params"] = params
            step["_uuid"] = uid
            out.append(step)
            i += 1
        return out

    @staticmethod
    def block_end(acts, start):
        g = acts[start]["WFWorkflowActionParameters"]["GroupingIdentifier"]
        for j in range(start + 1, len(acts)):
            p = acts[j].get("WFWorkflowActionParameters", {})
            if p.get("GroupingIdentifier") == g and p.get("WFControlFlowMode") == 2:
                return j
        return start  # unclosed: treated as a plain action

    def block(self, acts):
        """A menu, If or repeat as one step, or None when it isn't one build writes."""
        first, last = acts[0], acts[-1]
        if len(acts) < 2:
            return None
        ident = first["WFWorkflowActionIdentifier"].removeprefix(PREFIX)
        p0 = first["WFWorkflowActionParameters"]
        pe = last["WFWorkflowActionParameters"]
        g = p0["GroupingIdentifier"]
        # the markers between: mode 1 actions of this block, at its own level
        branches, current, depth_groups = [], [], []
        for a in acts[1:-1]:
            p = a.get("WFWorkflowActionParameters", {})
            if p.get("GroupingIdentifier") == g and p.get("WFControlFlowMode") == 1:
                branches.append((p, current))
                current = []
                continue
            current.append(a)
        if ident == "choosefrommenu":
            titles = p0.get("WFMenuItems", [])
            if set(p0) - {"GroupingIdentifier", "WFControlFlowMode", "WFMenuItems", "WFMenuPrompt"} or \
               not branches or branches[0][1] or [b[0].get("WFMenuItemTitle") for b in branches] != titles or \
               any(not isinstance(t, str) for t in titles):
                return None
            items, bodies = {}, [b[1] for b in branches[1:]] + [current]
            for t, body in zip(titles, bodies):
                items[t] = self.steps(body)
            step = {"menu": {"prompt": p0.get("WFMenuPrompt", ""), "items": items}}
        elif ident == "conditional":
            names = {v: k for k, v in Builder.CONDITIONS.items()}
            inp = p0.get("WFInput", {})
            if set(p0) - {"GroupingIdentifier", "WFControlFlowMode", "WFCondition", "WFInput",
                          "WFConditionalActionString", "WFNumberValue"} or p0.get("WFCondition") not in names or \
               inp.get("Type") != "Variable" or len(branches) > 1:
                return None
            try:
                cond = {"input": self.value(inp.get("Variable", {}), "WFInput"), "condition": names[p0["WFCondition"]]}
                if "WFNumberValue" in p0:
                    cond["value"] = p0["WFNumberValue"]
                elif "WFConditionalActionString" in p0:
                    cond["value"] = self.value(p0["WFConditionalActionString"], "WFConditionalActionString")
            except Unspecable:
                return None
            then = branches[0][1] if branches else current
            step = {"if": cond, "then": self.steps(then)}
            if branches:
                step["else"] = self.steps(current)
        elif ident in ("repeat.count", "repeat.each"):
            if branches:
                return None
            if ident == "repeat.count":
                if set(p0) - {"GroupingIdentifier", "WFControlFlowMode", "WFRepeatCount"}:
                    return None
                step = {"repeat": p0.get("WFRepeatCount", 1)}
            else:
                if set(p0) - {"GroupingIdentifier", "WFControlFlowMode", "WFInput"}:
                    return None
                try:
                    step = {"repeat_each": self.value(p0.get("WFInput"), "WFInput")}
                except Unspecable:
                    return None
            step["do"] = self.steps(current)
        else:
            return None
        if set(pe) - {"GroupingIdentifier", "WFControlFlowMode", "UUID"}:
            return None
        uid = pe.get("UUID")
        if uid in self.ids:
            step["id"] = self.ids[uid]
        step["_uuid"] = uid
        return step

    def finish(self, steps):
        """Drop the bookkeeping, keeping a step's UUID only when $raw links to it."""
        for s in steps:
            uid = s.pop("_uuid", None)
            if uid and uid in self.keep:
                s["uuid"] = uid
            for k in ("then", "else", "do"):
                if k in s:
                    self.finish(s[k])
            if "menu" in s:
                for body in s["menu"]["items"].values():
                    self.finish(body)
        return steps

    def spec(self, name):
        steps = self.finish(self.steps(self.acts))
        sp = {"name": name}
        icon = self.wf.get("WFWorkflowIcon", {})
        if icon:
            sp["icon"] = {"glyph": icon.get("WFWorkflowIconGlyphNumber"), "color": icon.get("WFWorkflowIconStartColor")}
        if self.wf.get("WFWorkflowTypes"):
            sp["types"] = self.wf["WFWorkflowTypes"]
        qs = []
        for q in self.wf.get("WFWorkflowImportQuestions", []):
            uid = self.acts[q["ActionIndex"]]["WFWorkflowActionParameters"].get("UUID")
            qs.append({"action": self.ids.get(uid, f"#{q['ActionIndex']}"), "param": q.get("ParameterKey"),
                       "text": q.get("Text", ""), "default": q.get("DefaultValue", "")})
        if qs:
            sp["import_questions"] = qs
        sp["actions"] = steps
        return sp


def to_spec(in_path, out_path):
    """Write a spec for a shortcut, signed or not: the reverse of build."""
    if os.path.exists(out_path):
        raise SystemExit(f"error: {out_path} exists; spec never overwrites a file (move it, or name another)")
    with tempfile.TemporaryDirectory() as tmp:
        wf, signer = unpack(in_path, tmp)
    name = os.path.splitext(os.path.basename(in_path))[0]
    sp = Decompiler(wf).spec(name)
    raw = json.dumps(sp).count('"$raw"')
    with open(out_path, "w") as f:
        json.dump(sp, f, indent=2, ensure_ascii=False)
        f.write("\n")
    print(f"wrote {out_path}: {len(wf.get('WFWorkflowActions', []))} actions as {len(sp['actions'])} steps"
          + (f"; {raw} value(s) kept as $raw" if raw else ""))
    return 0

def main(argv):
    if len(argv) < 3 or argv[1] not in ("build", "check", "sign", "verify", "decode", "spec"):
        print(__doc__)
        return 2
    cmd = argv[1]
    if cmd == "build" and len(argv) == 4:
        return build(argv[2], argv[3])
    if cmd == "check":
        return check(argv[2])
    if cmd == "sign" and len(argv) == 4:
        return sign(argv[2], argv[3])
    if cmd == "verify":
        against = argv[argv.index("--against") + 1] if "--against" in argv else None
        return verify(argv[2], against)
    if cmd == "decode" and len(argv) == 4:
        return decode(argv[2], argv[3])
    if cmd == "spec" and len(argv) == 4:
        return to_spec(argv[2], argv[3])
    print(__doc__)
    return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv))
