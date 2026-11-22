const status = document.querySelector<HTMLParagraphElement>("#status");

async function checkApi() {
  if (!status) return;
  try {
    const response = await fetch("/api/health");
    const result: { status: string } = await response.json();
    status.textContent = result.status === "ok" ? "服务已就绪" : "服务状态异常";
  } catch {
    status.textContent = "服务暂不可用，请稍后重试";
  }
}

void checkApi();
