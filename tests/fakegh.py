"""A fake GitHub API for testing hq end to end without the internet.

Repos are bare git repos under ROOT/<owner>/<name>.git (hq clones them through HQ_GIT_BASE=file://ROOT/).
Issues, comments, labels, collaborators and releases live in memory. /merges does a real git merge.
usage: python3 fakegh.py ROOT PORT
"""
import base64, json, os, re, subprocess, sys, tempfile, time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlparse, parse_qs

ROOT, PORT = sys.argv[1], int(sys.argv[2])
ME = {"login": "arthur", "avatar_url": "https://avatars.githubusercontent.com/u/1?v=4"}
now = lambda: time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
issues, comments, labels, files, collabs, invites, releases = {}, {}, {}, {}, {}, {}, {}
next_id = [1000]


def git(repo, *args):
    return subprocess.run(["git", "--git-dir", bare(repo), *args], capture_output=True, text=True)


def bare(repo):
    return os.path.join(ROOT, repo + ".git")


def user(login):
    return {"login": login, "avatar_url": f"https://avatars.githubusercontent.com/{login}?v=4"}


class H(BaseHTTPRequestHandler):
    def log_message(self, *a):
        pass

    def send(self, code, obj=None):
        body = b"" if obj is None else json.dumps(obj).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def body(self):
        n = int(self.headers.get("Content-Length") or 0)
        return json.loads(self.rfile.read(n) or b"{}") if n else {}

    def do_GET(self): self.route("GET")
    def do_POST(self): self.route("POST")
    def do_PATCH(self): self.route("PATCH")
    def do_PUT(self): self.route("PUT")
    def do_DELETE(self): self.route("DELETE")

    def route(self, m):
        if self.path.startswith("/dl/"):  # release asset downloads (public, no auth)
            f = os.path.join(os.environ.get("FAKE_DL", "/nonexistent"), os.path.basename(self.path))
            if not os.path.isfile(f):
                return self.send(404, {"message": "Not Found"})
            data = open(f, "rb").read()
            self.send_response(200); self.send_header("Content-Length", str(len(data))); self.end_headers()
            return self.wfile.write(data)
        ml = re.match(r"^/repos/([^/]+/[^/]+)/releases/latest$", self.path)
        if ml:  # public repo: no auth needed
            rs = releases.get(ml.group(1), [])
            return self.send(200, rs[0]) if rs else self.send(404, {"message": "Not Found"})
        if self.headers.get("Authorization") != "Bearer good-token":
            return self.send(401, {"message": "Bad credentials"})
        u = urlparse(self.path)
        q = {k: v[0] for k, v in parse_qs(u.query).items()}
        p = u.path
        b = self.body() if m in ("POST", "PATCH", "PUT") else {}
        if p == "/user":
            return self.send(200, ME)
        if p == "/user/repos" and m == "POST":
            r = "arthur/" + b["name"]
            subprocess.run(["git", "init", "-q", "--bare", "-b", "main", bare(r)])
            return self.send(201, {"full_name": r})
        mm = re.match(r"^/repos/([^/]+/[^/]+)(/.*)?$", p)
        if not mm:
            return self.send(404, {"message": "Not Found"})
        repo, rest = mm.group(1), mm.group(2) or ""
        if not os.path.isdir(bare(repo)):
            return self.send(404, {"message": "Not Found"})
        if rest == "":
            return self.send(200, {"full_name": repo, "private": True, "owner": {"login": repo.split("/")[0]}})
        if rest == "/labels":
            ls = labels.setdefault(repo, set())
            if b["name"] in ls:
                return self.send(422, {"message": "already_exists"})
            ls.add(b["name"])
            return self.send(201, b)
        if rest.startswith("/contents/"):
            path = rest[len("/contents/"):]
            key = (repo, path)
            if m == "GET":
                if key not in files:
                    return self.send(404, {"message": "Not Found"})
                c, sha = files[key]
                return self.send(200, {"sha": sha, "content": base64.b64encode(c).decode()})
            if key in files and b.get("sha") != files[key][1]:
                return self.send(409, {"message": "sha mismatch"})
            files[key] = (base64.b64decode(b["content"]), "sha%d" % time.time_ns())
            return self.send(200, {})
        if rest == "/issues" and m == "POST":
            n = len(issues.setdefault(repo, {})) + 1
            it = {"number": n, "title": b["title"], "body": b.get("body", ""), "state": "open", "user": ME,
                  "labels": [{"name": l} for l in b.get("labels", [])], "assignees": [user(a) for a in b.get("assignees", [])],
                  "comments": 0, "html_url": f"https://github.com/{repo}/issues/{n}", "created_at": now(), "updated_at": now()}
            issues[repo][n] = it
            return self.send(201, it)
        if rest == "/issues":
            st = q.get("state", "open")
            out = [i for i in issues.get(repo, {}).values() if st == "all" or i["state"] == st]
            page, per = int(q.get("page", 1)), int(q.get("per_page", 30))
            return self.send(200, out[(page - 1) * per: page * per])
        mi = re.match(r"^/issues/(\d+)(/comments)?$", rest)
        if mi:
            n = int(mi.group(1))
            it = issues.get(repo, {}).get(n)
            if not it:
                return self.send(404, {"message": "Not Found"})
            if mi.group(2):
                cs = comments.setdefault((repo, n), [])
                if m == "POST":
                    next_id[0] += 1
                    c = {"id": next_id[0], "body": b["body"], "user": ME, "created_at": now()}
                    cs.append(c)
                    it["comments"] = len(cs)
                    return self.send(201, c)
                page, per = int(q.get("page", 1)), int(q.get("per_page", 30))
                return self.send(200, cs[(page - 1) * per: page * per])
            if m == "PATCH":
                for k in ("title", "body", "state"):
                    if k in b:
                        it[k] = b[k]
                if "labels" in b:
                    it["labels"] = [{"name": l} for l in b["labels"]]
                if "assignees" in b:
                    it["assignees"] = [user(a) for a in b["assignees"]]
                it["updated_at"] = now()
            return self.send(200, it)
        mc = re.match(r"^/collaborators(?:/([^/?]+))?$", rest)
        if mc:
            cs = collabs.setdefault(repo, {"arthur"})
            if m == "GET":
                return self.send(200, [user(c) for c in sorted(cs)])
            who = mc.group(1)
            if m == "PUT":
                invites.setdefault(repo, set()).add(who)
                return self.send(201, {"id": 1})
            cs.discard(who)
            invites.get(repo, set()).discard(who)
            return self.send(204)
        if rest == "/invitations":
            return self.send(200, [{"id": i, "invitee": user(w)} for i, w in enumerate(sorted(invites.get(repo, set())))])
        if rest == "/releases":
            return self.send(200, releases.get(repo, []))
        if rest == "/pulls":
            return self.send(200, [])
        mcmp = re.match(r"^/compare/([^.]+)\.\.\.(.+)$", rest)
        if mcmp:
            r = git(repo, "rev-list", "--count", f"{mcmp.group(1)}..{mcmp.group(2)}")
            if r.returncode:
                return self.send(404, {"message": "Not Found"})
            return self.send(200, {"ahead_by": int(r.stdout.strip())})
        if rest == "/commits":
            r = git(repo, "log", q.get("sha", "HEAD"), "-n", q.get("per_page", "10"), "--format=%H%x1f%an%x1f%aI%x1f%s")
            if r.returncode:
                return self.send(409, {"message": "Git Repository is empty."})
            out = []
            for line in r.stdout.strip().splitlines():
                h, an, d, s = line.split("\x1f")
                out.append({"sha": h, "html_url": f"https://github.com/{repo}/commit/{h}",
                            "commit": {"message": s, "author": {"name": an, "date": d}}, "author": user(an)})
            return self.send(200, out)
        if rest == "/merges" and m == "POST":
            base, head = b["base"], b["head"]
            if git(repo, "merge-base", "--is-ancestor", head, base).returncode == 0:
                return self.send(204)
            with tempfile.TemporaryDirectory() as d:
                run = lambda *a: subprocess.run(["git", "-C", d, *a], capture_output=True, text=True)
                subprocess.run(["git", "clone", "-q", "-b", base, bare(repo), d])
                run("config", "user.name", "github"); run("config", "user.email", "gh@x")
                if run("merge", "--no-ff", "-m", b.get("commit_message", "merge"), "origin/" + head).returncode:
                    return self.send(409, {"message": "Merge conflict"})
                run("push", "-q", "origin", base)
                sha = run("rev-parse", "HEAD").stdout.strip()
            return self.send(201, {"sha": sha})
        return self.send(404, {"message": "Not Found: " + rest})


if __name__ == "__main__":
    # seed a release so the dashboard has numbers
    releases["arthur/shitboxrally-releases"] = [{"tag_name": "v0.6.5.3", "name": "0.6.5.3", "prerelease": False, "html_url": "x",
                                                 "published_at": now(), "assets": [{"download_count": 812}, {"download_count": 40}]}]
    if os.environ.get("FAKE_TAG"):
        releases["arthur/devsuite"] = [{"tag_name": os.environ["FAKE_TAG"], "body": "new stuff", "html_url": "x", "assets": [
            {"name": n, "browser_download_url": f"http://127.0.0.1:{PORT}/dl/{n}"} for n in ("hq.exe", "hq-linux", "hq-macos")]}]
    ThreadingHTTPServer(("127.0.0.1", PORT), H).serve_forever()
