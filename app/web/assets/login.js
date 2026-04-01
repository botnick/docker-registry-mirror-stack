const form = document.getElementById("login-form");
const errorNode = document.getElementById("login-error");

form?.addEventListener("submit", async (event) => {
  event.preventDefault();
  errorNode.classList.add("hidden");
  errorNode.textContent = "";

  const formData = new FormData(form);
  const payload = {
    username: String(formData.get("username") || ""),
    password: String(formData.get("password") || ""),
  };

  const response = await fetch("/auth/login", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });

  const data = await response.json().catch(() => ({}));
  if (!response.ok) {
    errorNode.textContent = data.error || "เข้าสู่ระบบไม่สำเร็จ";
    errorNode.classList.remove("hidden");
    return;
  }

  window.location.href = data.redirect || "/dashboard";
});
