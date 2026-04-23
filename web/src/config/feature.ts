// 前端 feature flag。
//
// 统一管理"功能是否对用户可见"的开关。当前版本已默认开放文字模型与
// mixed-mode 入口;如果需要临时隐藏,改成 false 后重新 build 即可。
export const ENABLE_CHAT_MODEL = true

// 对话框生图入口。前端默认展示,后端仍需同时开启 gateway.chat_image_mixed_enabled。
export const ENABLE_CHAT_IMAGE_MIXED = true
