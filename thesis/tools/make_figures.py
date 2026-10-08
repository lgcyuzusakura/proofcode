"""Generate original engineering diagrams as editable SVG and print PNG."""
from pathlib import Path
from PIL import Image, ImageDraw, ImageFont
import html
import math

ROOT = Path(__file__).resolve().parents[1]
OUTPUT = ROOT / "figures"
FONT = "C:/Windows/Fonts/msyh.ttc"


class Diagram:
    def __init__(self, width, height):
        self.w, self.h = width, height
        self.im = Image.new("RGB", (width, height), "white")
        self.d = ImageDraw.Draw(self.im)
        self.svg = [f'<svg xmlns="http://www.w3.org/2000/svg" width="{width}" height="{height}" viewBox="0 0 {width} {height}"><rect width="100%" height="100%" fill="white"/>']

    def text(self, x, y, text, size=31, color="#171717"):
        font = ImageFont.truetype(FONT, size)
        lines = text.split("\n")
        step = size * 1.35
        for i, line in enumerate(lines):
            yy = y + (i - (len(lines) - 1) / 2) * step
            self.d.text((x, yy), line, font=font, fill=color, anchor="mm")
            self.svg.append(f'<text x="{x}" y="{yy + size * .33}" font-size="{size}" font-family="Microsoft YaHei, sans-serif" text-anchor="middle" fill="{color}">{html.escape(line)}</text>')

    def box(self, xy, text, size=31, gray=False):
        x1, y1, x2, y2 = xy
        fill = "#eeeeee" if gray else "#ffffff"
        self.d.rounded_rectangle(xy, radius=12, outline="#333333", width=3, fill=fill)
        self.svg.append(f'<rect x="{x1}" y="{y1}" width="{x2-x1}" height="{y2-y1}" rx="12" fill="{fill}" stroke="#333333" stroke-width="3"/>')
        self.text((x1 + x2)/2, (y1 + y2)/2, text, size)

    def arrow(self, points, label=None, label_pos=None):
        self.d.line(points, fill="#333333", width=3)
        self.svg.append('<polyline points="' + " ".join(f"{x},{y}" for x,y in points) + '" fill="none" stroke="#333333" stroke-width="3"/>')
        x, y = points[-1]
        px, py = points[-2]
        angle = math.atan2(y-py, x-px)
        shape = [(x,y), (x-17*math.cos(angle-.4), y-17*math.sin(angle-.4)), (x-17*math.cos(angle+.4), y-17*math.sin(angle+.4))]
        self.d.polygon(shape, fill="#333333")
        self.svg.append('<polygon points="' + " ".join(f"{a},{b}" for a,b in shape) + '" fill="#333333"/>')
        if label:
            self.text(*label_pos, label, 25)

    def save(self, name):
        OUTPUT.mkdir(parents=True, exist_ok=True)
        self.im.save(OUTPUT / f"{name}.png", dpi=(300,300))
        (OUTPUT / f"{name}.svg").write_text("\n".join(self.svg) + "\n</svg>\n", encoding="utf-8")


def architecture():
    d=Diagram(1500,970)
    d.box((390,20,1110,125),"React Web 与 Wails 桌面客户端\n任务和批准界面",32,True)
    d.arrow([(750,125),(750,180)],"用户 API 与事件",(980,152))
    d.box((170,180,1330,300),"Spring Boot 控制平面\n项目归属   任务与批准   数据计划   实验比较",32,True)
    for center,label in [(320,"PostgreSQL\nTask  Outbox  Audit"),(750,"Redis\n任务取消提示"),(1180,"Artemis\n后台任务命令")]:
        d.arrow([(center,300),(center,365)])
        d.box((center-190,365,center+190,470),label,30)
    d.box((170,565,1330,695),"Go Runner 与 Agent Engine\n模型交互   工具执行   检索和压缩   Worktree 与 Checkpoint",32,True)
    d.arrow([(1180,470),(1180,565)],"领取任务",(1325,517))
    d.arrow([(70,630),(70,240),(170,240)],"",None)
    d.arrow([(170,630),(70,630)])
    d.text(88,520,"事件",25)
    for center,label in [(320,"生成模型\n代码与参数"),(750,"隔离 Git 工作树\n补丁与实际测试"),(1180,"项目数据网关\nPostgreSQL / Redis")]:
        d.arrow([(center,695),(center,770)])
        d.box((center-195,770,center+195,885),label,30)
    d.text(750,937,"桌面任务仍依赖控制平面和 Runner；数据修改复用用户批准",28)
    d.save("architecture")


def lifecycle():
    d=Diagram(1500,700)
    centers=[190,560,940,1310]
    names=["创建任务","事务 Outbox","QUEUED\n消息派发","RUNNING\n租约和 attempt"]
    for c,label in zip(centers,names): d.box((c-150,40,c+150,155),label,32,True)
    for left,right in zip(centers,centers[1:]): d.arrow([(left+150,98),(right-150,98)])
    d.box((1090,270,1480,385),"WAITING_APPROVAL\n保存上下文和计划",31)
    d.arrow([(1310,155),(1310,270)])
    d.box((570,270,980,385),"用户批准或拒绝\n恢复时校验配置",31)
    d.arrow([(1090,328),(980,328)])
    d.arrow([(775,270),(775,205),(1110,205),(1110,155)],"继续当前任务",(897,222))
    d.box((75,470,580,600),"实际验收与变更产物\n测试结果  Patch  Checkpoint",31,True)
    d.arrow([(1190,155),(1190,430),(330,430),(330,470)])
    d.box((760,470,1400,600),"终结事件\nSUCCEEDED / FAILED / CANCELLED",31,True)
    d.arrow([(580,535),(760,535)])
    d.text(750,664,"中断工具结果未知时保留状态；旧 attempt 不覆盖当前终结证据",28)
    d.save("lifecycle")


def retrieval():
    d=Diagram(1500,800)
    for c,label in [(250,"受限源码扫描\nHEAD 与双 manifest"),(750,"原文行块\n字节与哈希"),(1250,"任务查询\n可选失败反馈")]: d.box((c-225,30,c+225,140),label,31,True)
    d.arrow([(475,85),(525,85)])
    labels=["lexical\n词项线索","symbol\n声明名称","dependency\n文本引用","test\n测试线索"]
    for c,label in zip([195,565,935,1305],labels):
        d.box((c-160,250,c+160,365),label,32)
        d.arrow([(750,140),(750,200),(c,200),(c,250)])
    d.arrow([(1250,140),(1250,200),(750,200)])
    d.box((380,470,1120,580),"作用域过滤与 RRF 融合\n固定次序   整块预算筛选",32,True)
    for c in [195,565,935,1305]: d.arrow([(c,365),(c,420),(750,420),(750,470)])
    d.box((230,660,1270,765),"当前原文证据 → 主 Agent 模型视图\nproject  workspace  snapshot  path  line  byte  hash",29,True)
    d.arrow([(750,580),(750,660)])
    d.save("retrieval")


def context():
    d=Diagram(1500,795)
    d.box((30,30,585,150),"原始 Transcript\n完整工具调用与结果",32,True)
    d.box((900,30,1470,150),"持久化与批准恢复\n保持原记录",32,True)
    d.arrow([(585,90),(900,90)])
    d.box((30,255,585,365),"复制为本次模型视图",32)
    d.arrow([(305,150),(305,255)])
    d.box((900,255,1470,365),"重复成功纯日志引用替换\n保留最新完整副本",31)
    d.arrow([(585,310),(900,310)])
    d.box((900,470,1470,580),"注入本次源码证据\n再校验输入预算",31)
    d.arrow([(1185,365),(1185,470)])
    d.box((30,470,585,580),"按完整工具消息组裁剪\n超出不可裁剪预算则失败",31)
    d.arrow([(900,525),(585,525)])
    d.box((30,660,585,770),"模型请求与工具执行",32,True)
    d.arrow([(305,580),(305,660)])
    d.box((900,660,1470,770),"完整结果回到原始记录\n真实 usage 独立统计",31,True)
    d.arrow([(585,715),(900,715)])
    d.save("context")


def data():
    d=Diagram(1500,960)
    steps=[(270,30,"项目资源与结构快照"),(1230,30,"模型或用户提出 IR"),(270,245,"验证与确定性编译\n规范 IR  SQL  expectedRows"),(1230,245,"不可变计划与摘要\n绑定 task / attempt / schema"),(1230,485,"用户明确批准\nRunner 不能自批"),(270,485,"锁定任务与计划\n消费一次性执行授权"),(270,725,"写执行开始账本\n调用外部 PG 或 Redis"),(1230,725,"记录实际执行结果\n结果未知则禁止自动重放")]
    for x,y,t in steps:d.box((x-235,y,x+235,y+125),t,30,y in [30,725])
    d.arrow([(505,92),(995,92)])
    d.arrow([(1230,155),(1230,205),(270,205),(270,245)])
    d.arrow([(505,307),(995,307)])
    d.arrow([(1230,370),(1230,485)])
    d.arrow([(995,547),(505,547)])
    d.arrow([(270,610),(270,725)])
    d.arrow([(505,787),(995,787)])
    d.text(750,915,"生成、批准、执行开始和结束均记录审计；Redis 补偿另建批准计划",28)
    d.save("data")


def experiment():
    d=Diagram(1500,780)
    d.box((140,25,1360,140),"固定问题、模型、commit、temperature 与验收命令\n事务内创建 Experiment、六组 Run、Task 与 Outbox",31,True)
    for c,group,label in [(130,"A","纯文本"),(378,"B","工具策略"),(626,"C","Jev"),(874,"D","检索"),(1122,"E","检索去重"),(1370,"F","组合")]:
        d.box((c-110,250,c+110,365),group+"\n"+label,31)
        d.arrow([(750,140),(750,200),(c,200),(c,250)])
        d.arrow([(c,365),(c,420),(750,420),(750,480)])
    d.box((165,480,1335,590),"各组独立工作树与统一实际测试\n终结事件绑定配置版本、组别、commit 和当前 attempt",31,True)
    d.arrow([(750,590),(750,660)])
    d.box((165,660,1335,750),"逐 Run 证据与 JSON / CSV 比较   缺失指标保持 unknown",30,True)
    d.save("experiment")


if __name__ == "__main__":
    for make in [architecture,lifecycle,retrieval,context,data,experiment]: make()
    print("Created six original diagrams in thesis/figures")
