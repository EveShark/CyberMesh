import { useEffect, useMemo, useState } from "react";

interface Ember {
  id: number;
  x: number;
  size: number;
  duration: number;
  delay: number;
  opacity: number;
}

const EmberParticles = () => {
  const [isClient, setIsClient] = useState(false);

  useEffect(() => {
    setIsClient(true);
  }, []);

  const embers = useMemo(() => {
    if (!isClient) return [];
    
    const count = window.innerWidth < 768 ? 15 : 25;
    const particles: Ember[] = [];

    for (let i = 0; i < count; i++) {
      particles.push({
        id: i,
        x: Math.random() * 100,
        size: Math.random() * 4 + 2,
        duration: Math.random() * 8 + 6,
        delay: Math.random() * 5,
        opacity: Math.random() * 0.5 + 0.3,
      });
    }

    return particles;
  }, [isClient]);

  if (!isClient) return null;

  return (
    <div className="absolute inset-0 overflow-hidden pointer-events-none">
      {/* Gradient mesh background */}
      <div className="absolute inset-0 gradient-mesh opacity-60" />
      
      {/* Radial glow spots */}
      <div 
        className="absolute top-1/4 left-1/4 w-96 h-96 rounded-full blur-3xl"
        style={{ background: 'radial-gradient(circle, hsl(var(--ember) / 0.08) 0%, transparent 70%)' }}
      />
      <div 
        className="absolute bottom-1/4 right-1/4 w-80 h-80 rounded-full blur-3xl"
        style={{ background: 'radial-gradient(circle, hsl(var(--spark) / 0.06) 0%, transparent 70%)' }}
      />
      
      {/* Floating ember particles */}
      {embers.map((ember) => (
        <div
          key={ember.id}
          className="absolute rounded-full animate-ember-drift"
          style={{
            left: `${ember.x}%`,
            bottom: '-20px',
            width: `${ember.size}px`,
            height: `${ember.size}px`,
            background: `radial-gradient(circle, hsl(var(--ember) / ${ember.opacity}) 0%, hsl(var(--spark) / ${ember.opacity * 0.5}) 50%, transparent 100%)`,
            animationDuration: `${ember.duration}s`,
            animationDelay: `${ember.delay}s`,
            boxShadow: `0 0 ${ember.size * 2}px hsl(var(--ember) / ${ember.opacity * 0.5})`,
          }}
        />
      ))}

      {/* Subtle noise overlay */}
      <div className="absolute inset-0 noise-overlay opacity-30" />
    </div>
  );
};

export default EmberParticles;
