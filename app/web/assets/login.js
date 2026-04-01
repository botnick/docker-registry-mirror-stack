const form = document.getElementById("login-form");
const errorNode = document.getElementById("login-error");
const submitButton = form?.querySelector("button[type='submit']");

form?.addEventListener("submit", async (event) => {
  event.preventDefault();
  errorNode.classList.add("hidden");
  errorNode.textContent = "";
  if (submitButton) {
    submitButton.disabled = true;
    submitButton.textContent = "กำลังเข้าสู่ระบบ...";
  }

  const formData = new FormData(form);
  const payload = {
    username: String(formData.get("username") || ""),
    password: String(formData.get("password") || ""),
  };

  try {
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
  } catch (error) {
    errorNode.textContent = "เชื่อมต่อระบบไม่สำเร็จ กรุณาลองอีกครั้ง";
    errorNode.classList.remove("hidden");
  } finally {
    if (submitButton) {
      submitButton.disabled = false;
      submitButton.textContent = "เข้าสู่ระบบ";
    }
  }
});
