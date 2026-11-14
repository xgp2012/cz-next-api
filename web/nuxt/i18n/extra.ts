/**
 * 为语言包补充「旧实现里硬编码在组件内」的中文文案。
 *
 * 旧代码里有大量中文直接写在 JSX 中（渠道表单里的 Azure 提示、日志表的「充值/消费」、
 * 个人设置里的 toast 文案等）。重构时统一收拢到这里，zh / en 各一份，
 * 与 i18n/locales/*.json（从旧前端原样搬运的历史语言包）在运行时深合并。
 */
export const zhExtra: Record<string, any> = {
  errors: {
    not_found: {
      title: '页面不存在',
      description: '请检查你的浏览器地址是否正确',
    },
  },
  header: {
    menu: '导航',
  },
  common: {
    loading: '加载中...',
    confirm: '确认',
    cancel: '取消',
    save: '保存',
    delete: '删除',
    refresh: '刷新',
    search: '搜索',
    close: '关闭',
    detail: '详情',
    page: '页面',
    all: '全部',
    none: '无',
    not_test: '未测试',
    add: '添加',
    edit: '编辑',
    enabled: '已启用',
    disabled: '已禁用',
    unlimited: '无限制',
    click_to_view: '点击查看',
  },
  channel: {
    edit: {
      azure_model_notice:
        '注意：<strong>模型部署名称必须和模型名称保持一致</strong>，因为 One API 会把请求体中的模型参数替换为你的部署名称（模型名称中的点会被剔除），',
      azure_image_demo: '图片演示',
      azure_endpoint_placeholder:
        '请输入 AZURE_OPENAI_ENDPOINT，例如：https://docs-test-001.openai.azure.com',
      azure_api_version: '默认 API 版本',
      azure_api_version_placeholder:
        '请输入默认 API 版本，例如：2024-03-01-preview，该配置可以被实际的请求参数所覆盖',
      cloudflare_account_id: 'Account ID',
      cloudflare_account_placeholder:
        '请输入 Account ID，例如：d8d7c61dbc334c32d3ced580e4bf42b4',
      fastgpt_base_url: '私有部署地址',
      fastgpt_base_url_placeholder: '请输入私有部署地址，格式为：https://fastgpt.run/api/openapi',
      config_title: '渠道配置',
    },
  },
  token: {
    sort: {
      by: '排序',
    },
    messages: {
      copied_request_id: '已复制请求 ID：{{requestId}}',
      copy_request_failed: '请求 ID 复制失败：{{requestId}}',
    },
  },
  log: {
    type_name: {
      topup: '充值',
      usage: '消费',
      admin: '管理',
      system: '系统',
      test: '测试',
      unknown: '未知',
    },
  },
  user: {
    table: {
      unbound_email: '未绑定邮箱地址',
      delete_confirm: '删除用户',
    },
  },
  setting: {
    operation: {
      ratio: {
        model_invalid: '模型倍率不是合法的 JSON 字符串',
        completion_invalid: '补全倍率不是合法的 JSON 字符串',
        group_invalid: '分组倍率不是合法的 JSON 字符串',
      },
      log: {
        cleaned: '{{count}} 条日志已清理！',
        clean_failed: '日志清理失败：{{message}}',
      },
    },
    personal: {
      general: {
        token_reset: '令牌已重置并已复制到剪贴板',
        aff_copied: '邀请链接已复制到剪贴板',
        system_token_copied: '系统令牌已复制到剪贴板',
        deleted: '账户已删除！',
      },
      binding: {
        wechat_success: '微信账户绑定成功！',
        email_code_sent: '验证码发送成功，请检查邮箱！',
        email_success: '邮箱账户绑定成功！',
      },
      delete_account: {
        confirm_failed: '请输入你的账户名以确认删除！',
      },
    },
    other: {
      already_latest: '已是最新版本：{{version}}',
      new_version: '新版本：{{version}}',
      version_detail: '详情',
    },
  },
  oauth: {
    processing: '处理中...',
    bind_success: '绑定成功！',
    login_success: '登录成功！',
    retry: '出现错误，第 {{count}} 次重试中...',
    redirect_login: '操作失败，重定向至登录界面中...',
  },
  topup: {
    redeem_code: {
      placeholder_paste: '粘贴兑换码',
    },
  },
}

export const enExtra: Record<string, any> = {
  errors: {
    not_found: {
      title: 'Page not found',
      description: 'Please check that the browser address is correct',
    },
  },
  header: {
    menu: 'Navigation',
  },
  common: {
    loading: 'Loading...',
    confirm: 'Confirm',
    cancel: 'Cancel',
    save: 'Save',
    delete: 'Delete',
    refresh: 'Refresh',
    search: 'Search',
    close: 'Close',
    detail: 'Detail',
    page: 'page',
    all: 'All',
    none: 'None',
    not_test: 'Not tested',
    add: 'Add',
    edit: 'Edit',
    enabled: 'Enabled',
    disabled: 'Disabled',
    unlimited: 'Unlimited',
    click_to_view: 'Click to view',
  },
  channel: {
    edit: {
      azure_model_notice:
        'Note: <strong>the deployment name must match the model name</strong>, because One API replaces the model field in the request body with your deployment name (dots are stripped),',
      azure_image_demo: 'image demo',
      azure_endpoint_placeholder:
        'Enter AZURE_OPENAI_ENDPOINT, e.g. https://docs-test-001.openai.azure.com',
      azure_api_version: 'Default API version',
      azure_api_version_placeholder: 'e.g. 2024-03-01-preview, can be overridden by request parameters',
      cloudflare_account_id: 'Account ID',
      cloudflare_account_placeholder: 'Enter Account ID, e.g. d8d7c61dbc334c32d3ced580e4bf42b4',
      fastgpt_base_url: 'Private deployment URL',
      fastgpt_base_url_placeholder: 'e.g. https://fastgpt.run/api/openapi',
      config_title: 'Channel config',
    },
  },
  token: {
    sort: {
      by: 'Sort',
    },
    messages: {
      copied_request_id: 'Request ID copied: {{requestId}}',
      copy_request_failed: 'Failed to copy request ID: {{requestId}}',
    },
  },
  log: {
    type_name: {
      topup: 'Top up',
      usage: 'Usage',
      admin: 'Admin',
      system: 'System',
      test: 'Test',
      unknown: 'Unknown',
    },
  },
  user: {
    table: {
      unbound_email: 'No email bound',
      delete_confirm: 'Delete user',
    },
  },
  setting: {
    operation: {
      ratio: {
        model_invalid: 'Model ratio must be valid JSON',
        completion_invalid: 'Completion ratio must be valid JSON',
        group_invalid: 'Group ratio must be valid JSON',
      },
      log: {
        cleaned: '{{count}} log entries deleted!',
        clean_failed: 'Failed to clean logs: {{message}}',
      },
    },
    personal: {
      general: {
        token_reset: 'Token reset and copied to clipboard',
        aff_copied: 'Invitation link copied to clipboard',
        system_token_copied: 'System token copied to clipboard',
        deleted: 'Account deleted!',
      },
      binding: {
        wechat_success: 'WeChat account bound!',
        email_code_sent: 'Verification code sent, please check your email!',
        email_success: 'Email bound!',
      },
      delete_account: {
        confirm_failed: 'Please type your username to confirm deletion!',
      },
    },
    other: {
      already_latest: 'Already the latest version: {{version}}',
      new_version: 'New version: {{version}}',
      version_detail: 'Detail',
    },
  },
  oauth: {
    processing: 'Processing...',
    bind_success: 'Bound successfully!',
    login_success: 'Logged in successfully!',
    retry: 'Error occurred, retrying for the {{count}} time...',
    redirect_login: 'Operation failed, redirecting to the login page...',
  },
  topup: {
    redeem_code: {
      placeholder_paste: 'Paste redemption code',
    },
  },
}
