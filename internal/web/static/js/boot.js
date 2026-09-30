// Runs before first paint (classic script, no module): theme without a flash. Also the skip link.
try{var t=localStorage.getItem('lw:theme');if(t)document.documentElement.dataset.theme=t;var a=localStorage.getItem('lw:accent');if(a)document.documentElement.style.setProperty('--accent',a);var d=localStorage.getItem('lw:density');if(d)document.documentElement.dataset.density=d}catch(e){}
document.addEventListener('click',function(e){var l=e.target.closest&&e.target.closest('a.skip');if(l){e.preventDefault();document.getElementById('view').focus()}});
