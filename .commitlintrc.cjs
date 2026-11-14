// commitlint 配置。
//
// 为什么显式关闭 subject-case：本仓库的提交信息以中文为主、夹带英文技术名词
// （README、SQLite、go.mod、Docker…），而 subject-case 规则会把「句首出现连续
// 大写字母」判定为 sentence-case/upper-case 并报错。例如：
//   docs: README 镜像地址改为本仓库 GHCR
// 这句话在中文语境下完全正常，但会被判成 "subject must not be sentence-case"。
// 该规则服务于英文句子的书写习惯，对中英混排没有意义，只会制造噪声。
//
// 其余规则保持默认开启：type 必须来自约定集合、header 不超过 100 字符、
// 必须有 subject 等，这些才是真正需要守住的格式约束。
module.exports = {
  rules: {
    'subject-case': [0],
  },
}
