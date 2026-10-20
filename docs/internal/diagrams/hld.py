#!/usr/bin/env python3
# HLD evo CLI + MCP: иерархия слоёв и порядок шагов вызова (01-hld.svg) и
# диаграмма последовательности изменяющего вызова через MCP (02-sequence.svg).
# Раскладка задана вручную: у Graphviz нет управления ортогональными рёбрами
# между слоями. Ширина текста меряется по Liberation Sans (метрики Arial),
# скрипт падает, если подпись не помещается в блок.
# Запуск: python3 hld.py  (нужен Pillow и шрифты Liberation)
import os
import sys
from xml.sax.saxutils import escape

from PIL import ImageFont

FONTS = "/usr/share/fonts/truetype/liberation/"
_reg = {}


def font(size, bold=False):
    key = (size, bold)
    if key not in _reg:
        _reg[key] = ImageFont.truetype(FONTS + ("LiberationSans-Bold.ttf" if bold else "LiberationSans-Regular.ttf"), size * 4)
    return _reg[key]


def tw(text, size, bold=False):
    return font(size, bold).getlength(text) / 4


FAMILY = "Arial, 'Liberation Sans', Helvetica, sans-serif"

# Светлые цвета по умолчанию: файл читается и вне страницы. Страница
# перекрашивает их по классам под свою тему.
COL = {
    "actor": ("#F3EEF9", "#6B4FA0"), "surface": ("#E6F0FB", "#2F6DB5"),
    "core": ("#FFF4D6", "#B7860B"), "domain": ("#EAF6EE", "#2E8B57"),
    "client": ("#EEF1F4", "#5B6B7A"), "infra": ("#F6F6F4", "#8A8A80"),
    "store": ("#FBEFE8", "#B5653A"), "cloudnode": ("#E3F4F7", "#1F7A8C"),
    "guard": ("#FDECEA", "#C0392B"), "ext": ("#FFFFFF", "#8A8F96"),
}
ZONE = {
    "zone": ("none", "#9AA3AD"), "surfacezone": ("#F4F8FD", "#2F6DB5"),
    "corezone": ("#FFFBEF", "#B7860B"), "domainzone": ("#F3FAF5", "#2E8B57"),
    "cloud": ("#F1FAFB", "#1F7A8C"),
}
EDGE, EDGE_TEXT, MUTED, FG = "#5B6B7A", "#3B4650", "#566573", "#17212B"


class SVG:
    def __init__(self, w, h, label):
        self.w, self.h, self.label, self.out = w, h, label, []

    def add(self, s):
        self.out.append(s)

    def text(self, x, y, s, size=11, bold=False, anchor="start", cls="", fill=None):
        weight = ' font-weight="bold"' if bold else ""
        c = f' class="{cls}"' if cls else ""
        self.add(f'<text x="{x:.1f}" y="{y:.1f}" font-size="{size}"{weight} text-anchor="{anchor}" fill="{fill or FG}"{c}>{escape(s)}</text>')

    def zone(self, x, y, w, h, cls, label, dashed=False, lx=14):
        fill, stroke = ZONE[cls]
        dash = ' stroke-dasharray="6 4"' if dashed or cls == "zone" else ""
        self.add(f'<g class="cluster {cls}"><rect x="{x}" y="{y}" width="{w}" height="{h}" rx="10" fill="{fill}" stroke="{stroke}" stroke-width="1.2"{dash}/>')
        self.text(x + lx, y + 20, label, 12.5, True, fill=stroke, cls="zlabel")
        self.add("</g>")

    def box(self, x, y, w, h, cls, title, lines=(), badge=None, dashed=False, title_size=12.5):
        fill, stroke = COL[cls]
        dash = ' stroke-dasharray="5 3"' if dashed else ""
        self.add(f'<g class="node {cls}"><rect x="{x:.1f}" y="{y:.1f}" width="{w:.1f}" height="{h:.1f}" rx="8" fill="{fill}" stroke="{stroke}" stroke-width="1.3"{dash}/>')
        pad = 10
        tx = x + pad
        if badge:
            self.badge(tx + 9, y + 17, badge)
            tx += 23
        need = (tx - x) + tw(title, title_size, True) + pad
        check(need, w, title)
        self.text(tx, y + 21.5, title, title_size, True)
        for i, ln in enumerate(lines):
            check(tw(ln, 11) + 2 * pad, w, ln)
            self.text(x + pad, y + 39 + i * 15, ln, 11)
        check(39 + (len(lines) - 1) * 15 + 9 if lines else 30, h, title + " (высота)")
        self.add("</g>")

    def badge(self, cx, cy, n):
        self.add(f'<g class="badge"><circle cx="{cx:.1f}" cy="{cy:.1f}" r="9" fill="#1F7A8C"/>'
                 f'<text x="{cx:.1f}" y="{cy + 3.8:.1f}" font-size="10.5" font-weight="bold" text-anchor="middle" fill="#FFFFFF">{n}</text></g>')

    def edge(self, pts, label=None, lx=None, ly=None, anchor="start", dashed=False, cls="", head=True, lsize=10.5):
        d = "M" + " L".join(f"{x:.1f},{y:.1f}" for x, y in pts)
        dash = ' stroke-dasharray="5 4"' if dashed else ""
        mk = ' marker-end="url(#ah)"' if head else ""
        c = f"edge {cls}".strip()
        self.add(f'<g class="{c}"><path d="{d}" fill="none" stroke="{EDGE}" stroke-width="1.4"{dash}{mk}/>')
        if label:
            for i, part in enumerate(label.split("\n")):
                self.text(lx, ly + i * 13, part, lsize, anchor=anchor, fill=EDGE_TEXT, cls="elabel")
        self.add("</g>")

    def render(self):
        head = (f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {self.w} {self.h}" '
                f'font-family="{FAMILY}" role="img" aria-label="{escape(self.label)}">'
                '<defs><marker id="ah" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">'
                f'<path class="ahead" d="M0,0 L10,5 L0,10 z" fill="{EDGE}"/></marker></defs>'
                f'<rect class="canvas" x="0" y="0" width="{self.w}" height="{self.h}" fill="#FFFFFF"/>')
        return head + "".join(self.out) + "</svg>\n"


def check(need, have, what):
    if need > have + 0.5:
        sys.exit(f"не помещается ({need:.0f} > {have:.0f}): {what}")


# --------------------------------------------------------------------- HLD

def hld():
    s = SVG(1760, 1400, "HLD evo CLI + MCP: потребители, поверхности, реестр инструментов, "
                        "конвейер вызова (аргументы, конфиг, токен IAM, чтения, согласие, изменение), "
                        "исполнители и Cloud.ru")
    L, W = 56, 1544
    # колонки конвейера: шесть шагов по 230
    cw = 230
    gap = (W - 6 * cw) / 5
    col = [L + i * (cw + gap) for i in range(6)]
    cx = [c + cw / 2 for c in col]

    # 1. Потребители
    s.zone(28, 16, 1600, 104, "zone", "Потребители · шаг 1: запрос")
    cons = [("Инженер в терминале", "русская справка, превью, y/д"),
            ("Скрипты и CI", "--json, --yes, код 3 = нужно согласие"),
            ("AI-агент локально", "Claude Code, Cursor · stdio"),
            ("Сборщик внутренней системы", "image-factory mcpcall · stdio"),
            ("Удалённый MCP-клиент", "агент команды, n8n · HTTPS")]
    ax = [L + i * 316 for i in range(5)]
    for x, (t, l) in zip(ax, cons):
        s.box(x, 46, 280, 58, "actor", t, [l])

    # 2. Процесс evo
    s.zone(28, 140, 1600, 900, "surfacezone", "evo — один статический Go-бинарь", lx=214)
    s.box(L, 176, 596, 104, "surface", "CLI · internal/cli (cobra)", [
        "деревья vm, disk, vpc, image, k8s, pg, s3, secret — 72 инструмента",
        "evo tools call <любой из 152> · --json · стабильные коды выхода",
        "deploy · status · logs · verify · destroy — сразу в engine, мимо реестра",
        "configure · whoami · doctor — профиль, проверка ключа и окружения"])
    s.box(688, 176, 596, 104, "surface", "MCP · stdio · internal/mcp", [
        "JSON-RPC 2.0: initialize, tools/list, tools/call; ответы на английском",
        "процесс на сессию агента; stdout занят протоколом",
        "--toolsets сужает список для агента, права не меняет",
        "к confirmation_required добавляет поле approval для агента"])
    s.box(1320, 176, 280, 104, "surface", "MCP · Streamable HTTP", [
        "Bearer = MCP_BEARER_TOKEN",
        "Origin, сессии 24 ч, SSE",
        "≤ 32 вызова разом, ≤ 100 в пакете",
        "это ключ к evo, а не к Cloud.ru"], badge="2")
    s.box(1320, 300, 280, 86, "guard", "HTTP-политика", [
        "маскирует секреты в ответах",
        "kubectl/helm: фильтр глаголов и URL",
        "пути к файлам хоста закрыты"])
    for i, x in enumerate([ax[0] + 140, ax[1] + 140]):
        s.edge([(x, 104), (x, 176)])
    s.edge([(ax[2] + 140, 104), (ax[2] + 140, 176)])
    s.edge([(ax[3] + 140, 104), (ax[3] + 140, 176)])
    s.edge([(ax[4] + 140, 104), (ax[4] + 140, 176)])
    s.edge([(1460, 280), (1460, 300)])

    # 3. Реестр
    s.zone(44, 410, 1568, 196, "corezone",
           "Реестр инструментов · internal/tools — 152 инструмента, 23 группы, 65 меняют ресурсы")
    cards = [
        ("Вычисления · 21", "vm 5 · disk 7 · image 6 · snat 3", "→ compute.api"),
        ("Сеть · 31", "vpc 13 · nlb 9 · dns 9", "→ vpc.api · nlb.api · dns.api · compute.api"),
        ("Kubernetes · 15", "кластеры, пулы, плагины, kubectl, helm", "→ mk8s.api + kube-apiserver кластера"),
        ("Данные · 29", "pg 15 · kafka 7 · redis 7", "→ postgresql.api · kafka.api · redis.api"),
        ("Хранение и секреты · 23", "s3 7 · secret 6 · cert 10", "→ s3.cloud.ru · secretmanager · certificatemanager"),
        ("Контейнеры · 12", "ca 7 · ar 5", "→ containers.api · ar.api"),
        ("Наблюдение и деньги · 9", "monitoring 2 · audit 1 · billing 3 · iam 1 · org 2", "только чтение"),
        ("Сценарии · 12", "deploy 8 · stack 4", "engine, deployplan, stack → несколько API"),
    ]
    kw = (W - 3 * 24) / 4
    for i, (t, a, b) in enumerate(cards):
        r, c = divmod(i, 4)
        s.box(L + c * (kw + 24), 444 + r * 78, kw, 66, "core", t, [a, b])
    s.edge([(354, 280), (354, 410)], "вызов обработчика", 362, 350)
    s.edge([(986, 280), (986, 410)], "tools/call", 994, 350)
    s.edge([(1460, 386), (1460, 410)])

    # 4. Конвейер обработчика
    s.zone(44, 630, 1568, 186, "domainzone", "Что делает обработчик каждого инструмента — порядок одинаков для CLI и MCP", lx=220)
    steps = [
        ("3", "Аргументы", ["схема JSON + локальные правила", "ошибка → USAGE, в сеть ни шага"], "domain"),
        ("4", "Конфиг и проект", ["флаги > env > .evo.yaml > профиль", "MCP: проект из --allow-project"], "domain"),
        ("5", "Токен IAM", ["память → диск → обмен ключа", "обновить, если осталось < 5 мин"], "domain"),
        ("6", "Чтения для превью", ["GET ресурсов, подсетей, цены", "что создастся или удалится"], "domain"),
        ("7", "Согласие", ["нет confirmed → превью, стоп", "CLI: y/д или --yes", "MCP: агент спрашивает человека"], "core"),
        ("8", "Изменение и ожидание", ["POST · PATCH · DELETE", "шаг 9: async, operation_id,", "опрос *_operation с backoff"], "domain"),
    ]
    for i, (n, t, ls, cls) in enumerate(steps):
        s.box(col[i], 688, cw, 96, cls, t, ls, badge=n)
    for i in range(5):
        s.edge([(col[i] + cw, 736), (col[i + 1], 736)])
    s.edge([(120, 606), (120, 688)], "обработчик", 128, 664)
    # повтор после согласия
    s.edge([(cx[4], 688), (cx[4], 668), (200, 668), (200, 688)],
           "после согласия тот же вызов повторяется с confirmed=true", 960, 662, anchor="middle")
    # команды движка мимо реестра
    s.edge([(L, 262), (36, 262), (36, 760), (L, 760)], dashed=True)
    s.text(42, 400, "команды движка", 10.5, anchor="start", fill=EDGE_TEXT, cls="elabel")

    # 5. Исполнители и локальные данные
    s.box(col[0], 860, cw, 92, "infra", "Сквозные пакеты", [
        "errors: коды выхода, JSON", "i18n RU/EN · output · log", "санитайзер логов"], dashed=True)
    s.box(col[1], 860, cw, 92, "store", "Конфиг и состояние", [
        "config.yaml · .evo.yaml · env", ".evo/state.json + lock", "~/.evo: plans/, stack-jobs/"])
    s.box(col[2], 860, cw, 92, "store", "Ключ SA и кэш токена", [
        "credentials.yaml или", "CLOUDRU_KEY_ID / _SECRET", "кэш: token-<профиль>.json"])
    cl_x, cl_w = col[3], col[4] + cw - col[3]
    s.box(cl_x, 860, cl_w, 150, "client", "Клиенты REST · internal/<сервис>", [
        "provision (compute) · vpc · nlb · dns · mk8s · postgresql",
        "dbaas (Kafka, Redis) · secret · certmanager · containerapp",
        "artifact · monitoring · audit · billing · organization",
        "engine, deployplan и stack вызывают эти же клиенты",
        "httpretry: повтор чтений при 429/5xx, User-Agent evo/…",
        "в каждом запросе Authorization: Bearer <токен IAM>"])
    alt = [("s3 · minio-go", "SigV4 по отдельным S3-ключам"),
           ("deploy · SSH", "ключ ~/.evo/id_ed25519"),
           ("kubetools · kubectl, helm", "kubeconfig с JWT из id.cloud.ru")]
    ay = [860, 914, 968]
    for y, (t, l) in zip(ay, alt):
        s.box(col[5], y, cw, 48, "client", t, [l], dashed=True, title_size=12)
    s.edge([(cx[1], 784), (cx[1], 860)], "читает", cx[1] + 8, 826)
    s.edge([(cx[2], 784), (cx[2], 860)], "Token()", cx[2] + 8, 826)
    s.edge([(cx[3], 784), (cx[3], 860)], "GET + Bearer", cx[3] + 30, 826)
    s.badge(cx[3] + 18, 822, "6")
    s.edge([(col[5] + 50, 784), (col[5] + 50, 830), (cl_x + cl_w - 50, 830), (cl_x + cl_w - 50, 860)],
           "POST · DELETE + Bearer", cl_x + cl_w - 20, 848)
    s.badge(cl_x + cl_w - 32, 844, "8")
    s.edge([(col[5] + 170, 784), (col[5] + 170, 860)])
    s.text(col[5] + 178, 826, "прямые", 10.5, fill=EDGE_TEXT, cls="elabel")
    s.text(col[5] + 178, 839, "каналы", 10.5, fill=EDGE_TEXT, cls="elabel")

    # 6. Cloud.ru
    s.zone(560, 1080, 1068, 214, "cloud", "Cloud.ru Evolution")
    iam_x, iam_w = col[2], 210
    s.box(iam_x, 1116, iam_w, 158, "cloudnode", "IAM", [
        "iam.api.cloud.ru", "POST /api/v1/auth/token", "ключ SA → токен на ~1 ч",
        "id.cloud.ru: ключ → JWT", "проверяет права запросов", "к API продуктов"])
    s.box(cl_x, 1116, cl_w, 158, "cloudnode", "Публичные API продуктов · <product>.api.cloud.ru", [
        "compute · vpc · nlb · dns · mk8s · postgresql · kafka · redis",
        "secretmanager · certificatemanager · containers · ar",
        "monitoring · audit · billing · organization",
        "цена ВМ в превью — compute price-calculation",
        "каждый запрос: токен и права проверяются в IAM",
        "нет роли у сервисного аккаунта → 403 RBAC: access denied"])
    tg = [("kube-apiserver кластера", "проверяет JWT из kubeconfig"),
          ("ВМ из deploy", "sshd :22 · docker compose"),
          ("Object Storage", "s3.cloud.ru · без токена IAM")]
    ty = [1116, 1172, 1228]
    for y, (t, l) in zip(ty, tg):
        s.box(col[5], y, cw, 48, "cloudnode", t, [l], title_size=12)
    # ключ → IAM
    s.edge([(cx[2], 952), (cx[2], 1116)], "если кэш пуст\nили < 5 мин:\nPOST /auth/token", cx[2] - 10, 990, anchor="end")
    s.badge(cx[2] - 20, 972, "5")
    # клиенты → API
    s.edge([(cl_x + cl_w / 2, 1010), (cl_x + cl_w / 2, 1116)], "HTTPS + Bearer", cl_x + cl_w / 2 + 8, 1038)
    # API → IAM (проверка прав)
    s.edge([(cl_x, 1195), (iam_x + iam_w, 1195)], "права", cl_x - 6, 1188, anchor="end", lsize=10)
    # прямые каналы: вложенные пути справа без пересечений
    right = col[5] + cw
    pairs = [(ay[0] + 24, ty[2] + 24, 1666), (ay[1] + 24, ty[1] + 24, 1653), (ay[2] + 24, ty[0] + 24, 1640)]
    labels = ["S3 API", "SSH :22", "kube API"]
    for (y1, y2, x), lb in zip(pairs, labels):
        s.edge([(right, y1), (x, y1), (x, y2), (right, y2)])
    s.text(1672, 1060, "S3 · SSH ·", 10.5, fill=EDGE_TEXT, cls="elabel")
    s.text(1672, 1073, "kube", 10.5, fill=EDGE_TEXT, cls="elabel")
    # kubectl берёт JWT в IAM OIDC
    s.edge([(col[5] + 30, 1016), (col[5] + 30, 1058), (iam_x + iam_w - 30, 1058), (iam_x + iam_w - 30, 1116)],
           "JWT для kubeconfig (OIDC)", iam_x + iam_w - 22, 1052, dashed=True)
    # ВМ → Docker
    s.box(1180, 1322, 250, 58, "ext", "download.docker.com", ["apt, ключ встроен в evo"], dashed=True)
    s.edge([(col[5], ty[1] + 30), (1340, ty[1] + 30), (1340, 1322)], "apt install", 1346, 1312, lsize=10)

    # легенда
    lg_y = 1100
    s.text(L, lg_y, "Обозначения", 12.5, True)
    items = [("surface", "поверхности evo"), ("core", "реестр, согласие"), ("domain", "шаги обработчика"),
             ("store", "данные на машине с evo"), ("client", "клиенты и каналы"), ("cloudnode", "Cloud.ru"),
             ("guard", "защитный слой HTTP")]
    for i, (c, t) in enumerate(items):
        r, k = divmod(i, 2)
        x, y = L + k * 230, lg_y + 16 + r * 24
        f, st = COL[c]
        s.add(f'<g class="node {c}"><rect x="{x}" y="{y}" width="18" height="13" rx="3" fill="{f}" stroke="{st}" stroke-width="1.3"/></g>')
        s.text(x + 26, y + 11, t, 11)
    y = lg_y + 16 + 4 * 24
    s.edge([(L, y + 6), (L + 40, y + 6)])
    s.text(L + 48, y + 10, "вызов", 11)
    s.edge([(L + 230, y + 6), (L + 270, y + 6)], dashed=True)
    s.text(L + 278, y + 10, "мимо реестра, свой ключ", 11)
    s.badge(L + 9, y + 34, "5")
    s.text(L + 26, y + 38, "порядок шагов одного вызова", 11)
    return s.render()


# --------------------------------------------------------------- sequence

def sequence():
    lanes = [("Человек", "actor"), ("AI-агент", "actor"), ("evo · MCP HTTP", "surface"),
             ("Обработчик pg_create", "domain"), ("auth + кэш токена", "store"),
             ("IAM", "cloudnode"), ("postgresql.api / compute.api", "cloudnode")]
    lx = [100 + i * 225 for i in range(len(lanes))]
    msgs = [
        ("hdr", "Вызов 1: без confirmed — только чтения и превью"),
        (0, 1, "создай кластер PostgreSQL", False, None),
        (1, 2, "POST /mcp · tools/call pg_create · Bearer", False, "1"),
        (2, 2, "Bearer, Origin, сессия", False, "2"),
        (2, 3, "вызов обработчика", False, None),
        (3, 3, "схема аргументов, локальные правила", False, "3"),
        (3, 3, "конфиг, проект, --allow-project", False, "4"),
        (3, 4, "Token()", False, "5"),
        (4, 4, "в памяти или token-<профиль>.json < 5 мин? тогда:", False, None),
        (4, 5, "POST /api/v1/auth/token", False, None),
        (5, 4, "токен на ~1 ч", True, None),
        (4, 4, "сохранить в память и на диск (0600)", False, None),
        (4, 3, "токен", True, None),
        (3, 6, "GET подсети, спецификации · Bearer", False, "6"),
        (6, 5, "проверка токена и прав (RBAC)", False, None),
        (6, 3, "200 OK", True, None),
        (3, 1, "status: confirmation_required · превью · approval", True, "7"),
        (1, 0, "превью, просьба согласия", False, None),
        (0, 1, "да", True, None),
        ("hdr", "Вызов 2: confirmed=true — тот же путь, затем изменение"),
        (1, 2, "tools/call pg_create · confirmed=true", False, None),
        (2, 3, "шаги 2–6 заново", False, None),
        (4, 4, "токен из кэша, IAM не вызывается", False, None),
        (3, 6, "POST кластер · Bearer", False, "8"),
        (6, 5, "проверка токена и прав (RBAC)", False, None),
        (6, 3, "operation_id", True, None),
        (3, 1, "status: creating · operation_id", True, None),
        (1, 3, "pg_operation(operation_id, wait_seconds)", False, "9"),
        (3, 6, "GET операции с backoff до done", False, None),
    ]
    top, step = 96, 36
    h = top + len(msgs) * step + 40
    w = lx[-1] + 140
    s = SVG(w, h, "Последовательность изменяющего вызова через MCP: проверка Bearer, схема, конфиг, токен IAM из кэша "
                  "или обмен ключа, чтения для превью, confirmation_required, согласие человека, повтор с confirmed=true, "
                  "создание, опрос операции")
    for x, (t, cls) in zip(lx, lanes):
        bw = max(150, tw(t, 12.5, True) + 24)
        f, st = COL[cls]
        s.add(f'<g class="node {cls}"><rect x="{x - bw / 2:.1f}" y="20" width="{bw:.1f}" height="40" rx="8" fill="{f}" stroke="{st}" stroke-width="1.3"/></g>')
        s.text(x, 45, t, 12.5, True, anchor="middle")
        s.add(f'<g class="edge lifeline"><path d="M{x},60 L{x},{h - 16}" stroke="{EDGE}" stroke-width="1" stroke-dasharray="3 4" fill="none"/></g>')
    y = top
    for m in msgs:
        if m[0] == "hdr":
            s.add(f'<g class="edge divider"><path d="M20,{y - 6} L{w - 20},{y - 6}" stroke="{MUTED}" stroke-width="0.8" fill="none"/></g>')
            s.text(24, y + 10, m[1], 12, True, fill=MUTED, cls="zlabel")
            y += step
            continue
        a, b, label, ret, badge = m
        x1, x2 = lx[a], lx[b]
        if a == b:
            bw = tw(label, 11) + 20
            s.add(f'<g class="node domain"><rect x="{x1 + 12}" y="{y - 13}" width="{bw:.1f}" height="22" rx="5" fill="{COL["domain"][0]}" stroke="{COL["domain"][1]}" stroke-width="1"/></g>')
            s.text(x1 + 22, y + 2, label, 11)
            if badge:
                s.badge(x1 - 14, y - 2, badge)
            y += step
            continue
        lo, hi = min(x1, x2), max(x1, x2)
        check(tw(label, 11) + 16, hi - lo, label)
        s.edge([(x1, y), (x2 + (-6 if x2 > x1 else 6), y)], dashed=ret)
        s.text((x1 + x2) / 2, y - 6, label, 11, anchor="middle", fill=EDGE_TEXT, cls="elabel")
        if badge:
            # номер шага — с обратной стороны от стрелки, чтобы не задеть подпись
            s.badge(x1 + (-16 if x2 > x1 else 16), y, badge)
        y += step
    return s.render()


if __name__ == "__main__":
    here = os.path.dirname(os.path.abspath(__file__))
    for name, fn in (("01-hld.svg", hld), ("02-sequence.svg", sequence)):
        with open(os.path.join(here, name), "w", encoding="utf-8") as f:
            f.write(fn())
        print(name, "ok")
