import "./styles/tokens.css";
import App from "./App.svelte";
import { mount } from "svelte";

const target = document.getElementById("app");
if (!target) throw new Error("ES Studio: #app mount point missing");
// Remove the static boot marker embedded in index.html. Svelte 5's mount()
// appends rather than replaces, so leaving it would keep the placeholder on
// screen forever even after a successful start.
target.replaceChildren();
mount(App, { target });
