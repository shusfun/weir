module example.com/independent-consumer

go 1.25.0

require (
	github.com/shusfun/weir/packages/dsh v0.0.0
	github.com/shusfun/weir/packages/laya v0.0.0
)

replace github.com/shusfun/weir/packages/dsh => ../../packages/dsh
replace github.com/shusfun/weir/packages/laya => ../../packages/laya
