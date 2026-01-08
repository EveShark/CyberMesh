import type { Config } from "tailwindcss";

export default {
  darkMode: ["class"],
  content: ["./pages/**/*.{ts,tsx}", "./components/**/*.{ts,tsx}", "./app/**/*.{ts,tsx}", "./src/**/*.{ts,tsx}"],
  prefix: "",
  theme: {
    container: {
      center: true,
      padding: "1.5rem",
      screens: {
        "2xl": "1280px",
      },
    },
    extend: {
      fontFamily: {
        sans: ['Inter', 'system-ui', '-apple-system', 'sans-serif'],
      },
      colors: {
        border: "hsl(var(--border))",
        input: "hsl(var(--input))",
        ring: "hsl(var(--ring))",
        background: "hsl(var(--background))",
        foreground: "hsl(var(--foreground))",
        primary: {
          DEFAULT: "hsl(var(--primary))",
          foreground: "hsl(var(--primary-foreground))",
        },
        secondary: {
          DEFAULT: "hsl(var(--secondary))",
          foreground: "hsl(var(--secondary-foreground))",
        },
        destructive: {
          DEFAULT: "hsl(var(--destructive))",
          foreground: "hsl(var(--destructive-foreground))",
        },
        muted: {
          DEFAULT: "hsl(var(--muted))",
          foreground: "hsl(var(--muted-foreground))",
        },
        accent: {
          DEFAULT: "hsl(var(--accent))",
          foreground: "hsl(var(--accent-foreground))",
        },
        popover: {
          DEFAULT: "hsl(var(--popover))",
          foreground: "hsl(var(--popover-foreground))",
        },
        card: {
          DEFAULT: "hsl(var(--card))",
          foreground: "hsl(var(--card-foreground))",
        },
        // Fire & Ember tokens
        ember: {
          DEFAULT: "hsl(var(--ember))",
          glow: "hsl(var(--ember-glow))",
          deep: "hsl(var(--ember-deep))",
        },
        ash: {
          DEFAULT: "hsl(var(--ash))",
          light: "hsl(var(--ash-light))",
        },
        spark: {
          DEFAULT: "hsl(var(--spark))",
          glow: "hsl(var(--spark-glow))",
        },
        crimson: {
          DEFAULT: "hsl(var(--crimson))",
          glow: "hsl(var(--crimson-glow))",
        },
        frost: {
          DEFAULT: "hsl(var(--frost))",
          glow: "hsl(var(--frost-glow))",
        },
        fire: {
          DEFAULT: "hsl(var(--fire))",
          glow: "hsl(var(--fire-glow))",
        },
        cyber: {
          glow: "hsl(var(--cyber-glow))",
          surface: "hsl(var(--cyber-surface))",
          "surface-hover": "hsl(var(--cyber-surface-hover))",
        },
      },
      borderRadius: {
        lg: "var(--radius)",
        md: "calc(var(--radius) - 2px)",
        sm: "calc(var(--radius) - 4px)",
      },
      keyframes: {
        "accordion-down": {
          from: { height: "0" },
          to: { height: "var(--radix-accordion-content-height)" },
        },
        "accordion-up": {
          from: { height: "var(--radix-accordion-content-height)" },
          to: { height: "0" },
        },
        "fade-in-up": {
          from: { opacity: "0", transform: "translateY(20px)" },
          to: { opacity: "1", transform: "translateY(0)" },
        },
        "pulse-glow": {
          "0%, 100%": { opacity: "0.6" },
          "50%": { opacity: "1" },
        },
        "pulse-slow": {
          "0%, 100%": { opacity: "0.7", transform: "scale(1)" },
          "50%": { opacity: "1", transform: "scale(1.02)" },
        },
        "fill-bar-fast": {
          from: { width: "0%" },
          to: { width: "100%" },
        },
        "fill-bar-slow": {
          from: { width: "0%" },
          to: { width: "30%" },
        },
        "count-up": {
          from: { opacity: "0", transform: "translateY(10px)" },
          to: { opacity: "1", transform: "translateY(0)" },
        },
        "float": {
          "0%, 100%": { transform: "translateY(0px)" },
          "50%": { transform: "translateY(-8px)" },
        },
        "gradient-x": {
          "0%, 100%": { backgroundPosition: "0% 50%" },
          "50%": { backgroundPosition: "100% 50%" },
        },
        "blur-in": {
          from: { opacity: "0", filter: "blur(8px)", transform: "translateY(10px)" },
          to: { opacity: "1", filter: "blur(0px)", transform: "translateY(0)" },
        },
        "scale-up": {
          from: { opacity: "0", transform: "scale(0.95)" },
          to: { opacity: "1", transform: "scale(1)" },
        },
        "ember-drift": {
          "0%": { transform: "translateY(0) translateX(0) scale(1)", opacity: "0" },
          "10%": { opacity: "1" },
          "90%": { opacity: "0.6" },
          "100%": { transform: "translateY(-100vh) translateX(20px) scale(0.5)", opacity: "0" },
        },
        "glow-pulse": {
          "0%, 100%": { boxShadow: "0 0 20px hsl(var(--ember) / 0.3)" },
          "50%": { boxShadow: "0 0 40px hsl(var(--ember) / 0.6)" },
        },
      },
      animation: {
        "accordion-down": "accordion-down 0.2s ease-out",
        "accordion-up": "accordion-up 0.2s ease-out",
        "fade-in-up": "fade-in-up 0.5s ease-out forwards",
        "pulse-glow": "pulse-glow 3s ease-in-out infinite",
        "pulse-slow": "pulse-slow 3s ease-in-out infinite",
        "fill-bar-fast": "fill-bar-fast 0.6s ease-out forwards",
        "fill-bar-slow": "fill-bar-slow 2s ease-out forwards",
        "count-up": "count-up 0.4s ease-out forwards",
        "float": "float 5s ease-in-out infinite",
        "gradient-x": "gradient-x 6s ease infinite",
        "blur-in": "blur-in 0.6s ease-out forwards",
        "scale-up": "scale-up 0.5s ease-out forwards",
        "ember-drift": "ember-drift linear infinite",
        "glow-pulse": "glow-pulse 2s ease-in-out infinite",
      },
    },
  },
  plugins: [require("tailwindcss-animate")],
} satisfies Config;
