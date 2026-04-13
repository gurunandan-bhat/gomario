(function () {
    const profileForm = document.getElementById("profile-form");
    profileForm.addEventListener(
        "submit",
        (e) => {
            console.log("In listener");
            const imageInput = document.getElementById("profile-img");
            // Calculate total size
            let bytes = 0;
            for (const file of imageInput.files) {
                bytes += file.size;
            }
            if (bytes > 5 * 1024 * 1024) {
                imageInput.classList.add("is-invalid")
                e.preventDefault();
            }
        },
        false,
    );
})();
