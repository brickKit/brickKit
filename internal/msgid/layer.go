package msgid

// 三层文件（brickkit.yaml / deploy*.yaml / config/）共用的读取与解析文案。
const (
	LayerReadFailed        = "layer.read_failed"
	LayerNotValidYAML      = "layer.not_valid_yaml"
	LayerEmpty             = "layer.empty"
	LayerMultipleDocuments = "layer.multiple_documents"
	LayerWriteFailed       = "layer.write_failed"
	LayerEncodeFailed      = "layer.encode_failed"
)
