package tool

import (
	"encoding/binary"
	"hash/fnv"
)

// PointsStableTxnID 为积分入账生成稳定幂等键（与业务 ref 组合绑定），用于 Kafka 至少一次投递
// 与同步降级入账共用同一算法，避免「投递失败改同步」时产生第二笔 txn。
// salt：区分同 ref 下多次合法入账（如点赞者 user_id）；无则传 0。
func PointsStableTxnID(userID uint64, refType string, refID uint64, salt uint64) uint64 {
	h := fnv.New64a()
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], userID)
	_, _ = h.Write(buf[:])
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(refType))
	_, _ = h.Write([]byte{0})
	binary.LittleEndian.PutUint64(buf[:], refID)
	_, _ = h.Write(buf[:])
	binary.LittleEndian.PutUint64(buf[:], salt)
	_, _ = h.Write(buf[:])
	// 避免与 Snowflake 时间戳头部形态强绑定即可；全空间均匀分布
	id := h.Sum64()
	if id == 0 {
		return 1
	}
	return id
}
