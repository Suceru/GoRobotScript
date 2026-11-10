package GoRunner

import (
	"fmt"
	"math"
)

// ---------------------------------------------------------------------------
// 回放期识图对齐
//
// 录制期每次鼠标按下/松开都会截取一张以光标为中心的样本图 (32/64/128 像素)，
// 该帧同时记录了样本名与光标在样本图内的偏移，因此这一帧就是一个"关键点"。
//
// 回放期：
//  1. 关键点用图像模板匹配 (或颜色卷积匹配) 在当前画面上重新定位，
//     得到识别后的真实坐标 —— 起点与终点因此被移动到"最新的点"上；
//  2. 相邻两个关键点之间的整段鼠标路径，用一个相似变换 (旋转 + 等比拉伸 + 平移)
//     做对齐：该变换恰好把 起点→识别起点、终点→识别终点，因此路径两端严丝合缝，
//     段与段之间在关键点处连续，不会出现跳变；
//  3. 中间若关闭过识图 (Pause 切换)，关键点链在关闭处断开：关闭期间的路径按
//     原始录制坐标回放，下一轮识图的起点重新开始，不沿用上一轮的终点。
//
// 相似变换在只有两个对应点 (起点/终点) 时是唯一确定的，正好用满 4 个自由度：
//   缩放 s 与旋转 θ 由 (终点-起点) 的向量映射唯一解出，平移由起点映射解出。
// ---------------------------------------------------------------------------

// mousePosOps 带绝对坐标、需要参与路径对齐的动作类型
var mousePosOps = map[string]bool{
	"init": true,
	"mv":   true,
	"md":   true,
	"mu":   true,
}

// simTransform 相似变换：p' = q + M·(p - a)
//
//	a  = 关键点起点 (录制坐标)，q = 关键点起点 (识别坐标)
//	M  = 等比缩放 × 旋转，把 (b-a) 精确映射到 (rb-q)
type simTransform struct {
	ax, ay float64
	qx, qy float64
	m00    float64
	m01    float64
	m10    float64
	m11    float64

	scale      float64 // 等比缩放系数
	rawScale   float64 // 原始缩放系数 (可信度判定用这个)
	angle      float64 // 旋转角度 (度)
	degenerate bool    // 起终点重合，退化为纯平移
}

// newTranslation 纯平移变换 (保持路径形状不变，只做整体位移)
func newTranslation(ax, ay, qx, qy int) simTransform {
	return simTransform{
		ax: float64(ax), ay: float64(ay),
		qx: float64(qx), qy: float64(qy),
		m00: 1, m11: 1, scale: 1,
	}
}

// newSimTransform 由两组对应点构造相似变换。
//
// **不做限幅**：该变换的用途就是"把 录制起点→识别起点、录制终点→识别终点"，
// 两端必须严格对齐；缩放是否合理由调用方用 rawScale 判定（超出范围就整段不用），
// 而不是把它夹到区间里——夹了之后端点就对不上了（路径会少走一截，
// 到点击那一刻再跳过去）。
func newSimTransform(ax, ay, qx, qy, bx, by, rx, ry int) simTransform {
	t := simTransform{
		ax: float64(ax), ay: float64(ay),
		qx: float64(qx), qy: float64(qy),
		m00: 1, m11: 1, scale: 1,
	}
	vx := float64(bx - ax)
	vy := float64(by - ay)
	wx := float64(rx - qx)
	wy := float64(ry - qy)
	den := vx*vx + vy*vy
	if den < 1e-9 {
		// 起终点重合 (例如原地点击)：无方向可解，退化为纯平移
		t.degenerate = true
		return t
	}

	dot := vx*wx + vy*wy
	cross := vx*wy - vy*wx
	t.rawScale = math.Hypot(wx, wy) / math.Sqrt(den)

	t.m00 = dot / den
	t.m01 = -cross / den
	t.m10 = cross / den
	t.m11 = dot / den
	t.scale = t.rawScale
	t.angle = math.Atan2(cross, dot) * 180 / math.Pi
	return t
}

// 等比缩放的可用区间：超出即认为两端识别结果互相矛盾，不再做旋转/拉伸
const (
	simScaleMin = 0.5
	simScaleMax = 2.0
)

// DefaultVisionMaxRotation 允许的最大旋转角度（度）；默认 180 表示**不限制（整整一圈）**。
//
// 关于"360 度"：路径旋转的完整范围确实是一整圈，但数学上"旋转 θ"与"旋转 θ−360°"
// 是同一个旋转，所以用**有符号角**表示时值域天然就是 (−180°, +180°] ——
// 两端接起来正好是 360° 全圆。段日志里的角度就取这个约定：
//
//	+90°   = 顺时针四分之一圈
//	-159°  = 逆时针 159°（等价于顺时针 201°）
//
// 因此**限制值 ≥ 180 就等于不限制**（任何角度都满足 |angle| ≤ 180）。
//
// 什么时候该真的限制：界面**内容每轮重排**（例如舒尔特方格每轮随机打乱 9 个数字）时，
// 同一段路径的两个端点回放时本来就可能换到任意方位，旋转 90°、180° 都正常且必须照做 ——
// 限死旋转会让端点落不到识别位置上、点击偏格，所以保持默认。
//
// 而**固定布局**界面（元素不重排）出现大旋转基本只有一种可能：
// 两端里有一端打到了"长得几乎一样的另一个元素"。那时把本值调小（例如 -vrot 15），
// 就会自动放弃旋转/拉伸、退回纯平移，避免路径被瞬间横拉。
const DefaultVisionMaxRotation = 180.0

// apply 把录制坐标映射到识别后的坐标
func (t simTransform) apply(x, y int) (int, int) {
	dx := float64(x) - t.ax
	dy := float64(y) - t.ay
	return int(math.Round(t.qx + t.m00*dx + t.m01*dy)),
		int(math.Round(t.qy + t.m10*dx + t.m11*dy))
}

// visionAnchor 一个识图关键点。
//
// 所有字段由 visionPipeline 的互斥锁保护：预取协程写、回放主线程读。
type visionAnchor struct {
	frame    int    // 所在帧索引
	img      string // 样本名 (不含扩展名)
	iox, ioy int    // 光标在样本图内的偏移
	mode     string // image | color
	size     int    // 样本边长

	rawX, rawY int // 录制坐标
	tgtX, tgtY int // 识别坐标 (识别失败时等于录制坐标)

	resolved bool    // 是否已经确认过 (可参与对齐)
	early    bool    // 是否在关键点自身时刻之前被提前识别 (段终点)
	matched  bool    // 识别是否成功
	fast     bool    // 是否由"快速区域/预取"得到
	score    float64 // TM_SQDIFF_NORMED 分数 (越小越好)

	// 预取候选 (后台协程提前标出来的大概位置)
	hasCand   bool
	candX     int
	candY     int
	candScore float64
	candGen   int64 // 候选对应的预测世代
}

// segMode 路径段采用的变换策略
type segMode int

const (
	segIdentity segMode = iota // 两端都不可信：按原坐标回放
	segSimilar  segMode = iota // 旋转 + 等比拉伸 + 平移 (起终点都精确对齐)
	segTransA   segMode = iota // 仅起点可信：整体平移，形状保持不变
	segTransB   segMode = iota // 仅终点可信：整体平移，形状保持不变
)

func (m segMode) String() string {
	switch m {
	case segSimilar:
		return "旋转/拉伸对齐"
	case segTransA:
		return "纯平移对齐(起点可信)"
	case segTransB:
		return "纯平移对齐(终点可信)"
	default:
		return "按原坐标回放"
	}
}

// visionSeg 关键点之间的一段路径
type visionSeg struct {
	a, b      int // anchors 索引
	from, to  int // 帧范围 [from, to]
	ready     bool
	mode      segMode
	m         simTransform
	reason    string // 降级原因（写进日志，便于排查）
	scaleNote string // 位移比例超出常规但照样采用时的说明（写进日志）
	matchedA  bool
	matchedB  bool
}

// visionPlan 整条脚本的识图对齐方案
type visionPlan struct {
	anchors []visionAnchor
	links   []bool // links[i]: anchors[i] 与 anchors[i+1] 是否连通 (期间未关闭识图)
	segs    []visionSeg
	blockOf []int // 每帧归属的路径段索引，-1 = 原样回放

	anchorOf map[int]int // 帧索引 -> anchors 索引
}

// buildVisionPlan 预扫描全部帧，建立关键点、连通关系与路径段归属
func buildVisionPlan(frames []playFrame) *visionPlan {
	n := len(frames)
	p := &visionPlan{
		blockOf:  make([]int, n),
		anchorOf: make(map[int]int),
	}
	for i := range p.blockOf {
		p.blockOf[i] = -1
	}

	// 1. 收集识图关闭标记与关键点
	offAt := make([]bool, n)
	for i, f := range frames {
		if f.action == nil {
			continue
		}
		a := f.action
		switch a.Op {
		case "vision":
			if a.Von != nil && !*a.Von {
				offAt[i] = true
			}
		case "vslot":
			// 仅切换槽位，不打断关键点链
		default:
			if a.Vi != "" {
				p.anchorOf[i] = len(p.anchors)
				p.anchors = append(p.anchors, visionAnchor{
					frame: i, img: a.Vi, iox: a.Vx, ioy: a.Vy,
					mode: a.Vmode, size: a.Vsize,
					rawX: a.X, rawY: a.Y, tgtX: a.X, tgtY: a.Y,
				})
			}
		}
	}

	// 识图关闭的帧索引前缀和：countOff(from, to] = prefix[to+1] - prefix[from+1]
	prefix := make([]int, n+1)
	for i := 0; i < n; i++ {
		prefix[i+1] = prefix[i]
		if offAt[i] {
			prefix[i+1]++
		}
	}
	hasOffBetween := func(from, to int) bool {
		if from < 0 || to < 0 || from >= to {
			return false
		}
		return prefix[to+1]-prefix[from+1] > 0
	}

	if len(p.anchors) < 2 {
		return p
	}

	// 2. 连通关系：期间关闭过识图 => 断开 (下一轮起点重新开始)
	p.links = make([]bool, len(p.anchors)-1)
	for i := 0; i+1 < len(p.anchors); i++ {
		p.links[i] = !hasOffBetween(p.anchors[i].frame, p.anchors[i+1].frame)
	}

	// 3. 每个连通对构成一段待对齐路径
	for i := 0; i+1 < len(p.anchors); i++ {
		if !p.links[i] {
			continue
		}
		p.segs = append(p.segs, visionSeg{
			a: i, b: i + 1,
			from: p.anchors[i].frame,
			to:   p.anchors[i+1].frame,
		})
	}
	if len(p.segs) == 0 {
		return p
	}

	// 4. 帧 → 路径段 归属
	for si := range p.segs {
		seg := &p.segs[si]
		for i := seg.from; i <= seg.to; i++ {
			p.blockOf[i] = si
		}
	}

	// 5. 尾段外推：最后一个关键点之后，只要中途没关闭过识图，
	//    就沿用最后一段的变换，保证点击后的收尾路径同样连续、不跳变
	last := p.segs[len(p.segs)-1]
	lastIdx := len(p.segs) - 1
	for i := last.to + 1; i < n; i++ {
		if hasOffBetween(last.to, i) {
			break
		}
		p.blockOf[i] = lastIdx
	}
	return p
}

// usable 是否存在可用的对齐路径
func (p *visionPlan) usable() bool {
	return p != nil && len(p.segs) > 0
}

// predictionFor 为锚点 k 生成预测位置：用链路上最近的、已成功识别的锚点作为标记来源。
//
//	预测光标位置 = 来源锚点识别位置 + (本锚点录制坐标 - 来源锚点录制坐标)
//
// 调用方必须已持有 vp.mu。
func (p *visionPlan) predictionFor(k int) anchorPred {
	pr := anchorPred{}
	if src := p.prevResolvedAnchor(k); src >= 0 {
		s := &p.anchors[src]
		pr.x = s.tgtX + (p.anchors[k].rawX - s.rawX)
		pr.y = s.tgtY + (p.anchors[k].rawY - s.rawY)
		pr.has = true
		pr.witness = s
	}
	return pr
}

// prevResolvedAnchor 找到锚点 k 之前、同一条链上最近的成功识别锚点；没有则返回 -1
func (p *visionPlan) prevResolvedAnchor(k int) int {
	for i := k - 1; i >= 0; i-- {
		if i < len(p.links) && !p.links[i] {
			return -1 // 链路已被识图关闭打断，不跨链预测
		}
		if p.anchors[i].matched {
			return i
		}
	}
	return -1
}

// invalidateSegsUsing 关键点 k 的位置发生变化时，作废所有引用它的段变换。
//
// 段变换是用"两端的**当时**位置"算出来的：段终点是在段起点处提前标记的，
// 如果这个终点在自己的帧上被复核/重新定位改掉了位置，那么之前算好的段变换就
// **指向旧位置**（旧位置可能就是那个"长得像"的错误目标）。
// 继续用它会出现两种症状：
//   - 点击/松开那一刻用的是旧坐标 => 按下与松开脱节、点到的不是识别到的位置；
//   - 关键点之后的收尾路径按旧变换走 => 光标又跳回旧位置。
//
// 因此位置一变就让引用它的段作废，下次用到时按新位置重建。
func (p *visionPlan) invalidateSegsUsing(k int) {
	for si := range p.segs {
		if p.segs[si].a == k || p.segs[si].b == k {
			p.segs[si].ready = false
		}
	}
}

// activateSeg 在进入某段路径前解析两端关键点并构造变换。
// 段终点必须在这里提前确认，否则本段路径无法在播放前完成对齐。
//
// 变换可信度分级 (宁可不对齐，也不能乱跳)：
//  1. 两端都命中且等比缩放落在安全区间 => 旋转 + 拉伸 + 平移，起终点都精确对齐；
//  2. 两端都命中但缩放不可信 (两端识别结果互相矛盾) => 取分数更好的一端做纯平移；
//  3. 只有一端命中 => 该端做纯平移，保持路径形状；
//  4. 两端都没命中 => 原样回放。
func (p *visionPlan) activateSeg(si int, vp *visionPipeline) {
	seg := &p.segs[si]
	if seg.ready {
		return
	}
	// 注意：ready 在**算完变换之后**才置位。
	// 解析两端时可能发生"位置修正"（commit -> invalidateSegsUsing），
	// 若提前置位就会被那次修正放过，留下一个用旧位置算出来的变换。

	aIdx, bIdx := seg.a, seg.b
	vp.mu.Lock()
	if !p.anchors[bIdx].resolved {
		p.anchors[bIdx].early = true // 提前确认的标记：到达自身时刻要复核
	}
	vp.mu.Unlock()

	// 解析顺序构成"加速链"：
	//   起点 -> 用链路上一个已确认锚点预测；
	//   终点 -> 用本段起点（刚确认、最近、最准）预测。
	// 两者都优先使用后台预取好的候选，复核通过即采用；否则标准识别。
	vp.ResolveAnchorAtUse(aIdx)
	vp.ResolveAnchorAtUse(bIdx)

	vp.mu.Lock()
	a, b := p.anchors[aIdx], p.anchors[bIdx]
	seg.matchedA, seg.matchedB = a.matched, b.matched
	vp.mu.Unlock()

	switch {
	case a.matched && b.matched:
		s := newSimTransform(a.rawX, a.rawY, a.tgtX, a.tgtY, b.rawX, b.rawY, b.tgtX, b.tgtY)
		if s.degenerate {
			// 两端**录制位置重合**（同一次点击的按下/松开）：没有方向可解，
			// 相似变换无定义，只能纯平移（取匹配度更高那一端）。
			seg.reason = "两端录制位置重合(同一次点击) => 纯平移"
			if a.score >= b.score {
				seg.mode, seg.m = segTransA, newTranslation(a.rawX, a.rawY, a.tgtX, a.tgtY)
			} else {
				seg.mode, seg.m = segTransB, newTranslation(b.rawX, b.rawY, b.tgtX, b.tgtY)
			}
			break
		}
		// 可信度门限：两端识别结果必须"自洽"。
		//   旋转必须不超过 maxRotation —— 有符号角的值域就是 (−180°, +180°]，
		//   所以 maxRotation >= 180 等于不限制（整整一圈）。
		//   位移比例(缩放)默认也要求落在合理区间，但见下面的 trusted 例外。
		scaleOK := s.rawScale >= simScaleMin && s.rawScale <= simScaleMax
		rotOK := vp.maxRotation >= 180 || math.Abs(s.angle) <= vp.maxRotation
		// trusted：两端都是"高置信度命中"时，位移比例本来就可以任意。
		//
		// 内容每轮重排的界面（例如舒尔特方格每轮随机打乱）里，同一段的两端在新一轮
		// 可能落在任意两格 —— 录制时相距 300px、这一轮只相距 100px 都完全正常，
		// 比例 0.45x / 2.1x 并不是"两端矛盾"，而是这一轮的真实布局。
		// 此时若还拿比例去否决，路径就会按**录制时的旧位置**走（朝目标反方向），
		// 到点击那一刻才跳回正确位置。所以高置信度命中直接照做。
		trusted := vp.endpointTrusted(a) && vp.endpointTrusted(b)
		if rotOK && (scaleOK || trusted) {
			seg.mode, seg.m = segSimilar, s
			if !scaleOK {
				seg.scaleNote = fmt.Sprintf("位移比例%.3fx 超出常规 %.2f~%.2f，但两端都是高置信度命中 => 照做",
					s.rawScale, simScaleMin, simScaleMax)
			}
			break
		}
		if !scaleOK {
			seg.reason = fmt.Sprintf("缩放%.3fx 超出 %.2f~%.2f", s.rawScale, simScaleMin, simScaleMax)
		} else {
			seg.reason = fmt.Sprintf("旋转%.1f° 超出 ±%.0f°", s.angle, vp.maxRotation)
		}
		// 退回纯平移：保留匹配度更高那一端的成果作参考，另一端只回退它自己
		if a.score >= b.score {
			seg.mode, seg.m = segTransA, newTranslation(a.rawX, a.rawY, a.tgtX, a.tgtY)
		} else {
			seg.mode, seg.m = segTransB, newTranslation(b.rawX, b.rawY, b.tgtX, b.tgtY)
		}
	case a.matched:
		seg.mode, seg.m = segTransA, newTranslation(a.rawX, a.rawY, a.tgtX, a.tgtY)
	case b.matched:
		seg.mode, seg.m = segTransB, newTranslation(b.rawX, b.rawY, b.tgtX, b.tgtY)
	default:
		seg.mode, seg.m = segIdentity, newTranslation(0, 0, 0, 0)
	}

	// 本段两端已定：把预取窗口向前推
	vp.AfterSegmentResolved(si)
	seg.ready = true // 变换已按两端**当前**位置算好，可以用了

	fmt.Printf("[Vision] 路径段#%d 帧%d~%d  %s", si+1, seg.from, seg.to, seg.mode)
	if seg.reason != "" {
		fmt.Printf("  ← 放弃旋转/拉伸：%s", seg.reason)
	}
	fmt.Println()
	if seg.scaleNote != "" {
		fmt.Printf("         (%s)\n", seg.scaleNote)
	}
	fmt.Printf("         起点(%d,%d)->(%d,%d)%s  终点(%d,%d)->(%d,%d)%s",
		a.rawX, a.rawY, a.tgtX, a.tgtY, hitMark(a.matched, a.score, vp.matcher.thresholdPercent),
		b.rawX, b.rawY, b.tgtX, b.tgtY, hitMark(b.matched, b.score, vp.matcher.thresholdPercent))
	if seg.mode == segSimilar {
		fmt.Printf("  旋转%.1f° 缩放%.3fx", seg.m.angle, seg.m.scale)
	}
	fmt.Println()
}

// hitMark 生成日志里的命中标记。匹配度只比阈值高一点的会标注"偏低"，
// 提示这是一次"勉强通过"的匹配 —— 实际回放里这类命中往往是打到了
// 另一个长得相似的 UI 元素上，看到"偏低"就该考虑抬高 -vs。
func hitMark(ok bool, sim float64, threshold float64) string {
	if !ok {
		return " [未命中]"
	}
	if threshold < 100 && sim < threshold+5 {
		return fmt.Sprintf(" [匹配度 %.1f%% 偏低]", sim)
	}
	return fmt.Sprintf(" [匹配度 %.1f%%]", sim)
}

// ---------------------------------------------------------------------------
// 识别器
// ---------------------------------------------------------------------------

// DefaultVisionThreshold 默认匹配阈值 (TM_SQDIFF_NORMED，越小越严格)。
//
// 实测标定 (2560x1440 桌面，32x32 样本)：
//
//	同一块内容跨两次抓屏的真实命中 => 0.00000
//	完全无关的随机噪声模板在整屏搜索中的最低分 => 0.166 ~ 0.181
//	实际游戏回放中"匹配到了另一个相似 UI 元素"的假命中 => 0.060 ~ 0.095
//
// 第三项是实践中最危险的一档：分数明显低于随机最低分，**看起来像真命中**，
// 但位置是错的（UI 里有多个长得像的槽位/按钮），会把整段路径平移到错误位置。
// 因此默认阈值取 0.05 —— 明显比"假命中带"更严；真实命中几乎必然通过，
// 内容变化较大的画面宁可判为未命中（退化为按原坐标回放），也不乱跳。
// 需要放宽时用 -vt 调大（日志里分数超过阈值 60% 会标注"偏高"提示）。
const DefaultVisionThreshold = 0.05
