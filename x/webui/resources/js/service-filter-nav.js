/**
 * Service Filter Navigation Module
 *
 * Provides reusable keyboard navigation and service filtering for list-based tabs.
 * Manages state and keyboard handling for filtering items by service and navigating with arrow keys.
 *
 * Configuration object:
 * {
 *   container: HTMLElement,           // Container holding the list and services
 *   itemSelector: '.alert-item',      // CSS selector for items (must have data-service-name)
 *   serviceSelector: '.service-item', // CSS selector for service filter items (must have data-service)
 *   selectedClass: 'alert-selected',  // Class applied to selected item
 *   markedClass: 'alert-marked',      // Class applied to marked items
 *   markItemCallback: async (item) => {}, // Called when item should be marked as seen
 *   onStateChange: (state) => {}      // Called when state changes (for debugging)
 * }
 */

export class ServiceFilterNav {
    constructor(config) {
        this.config = config;
        this.container = config.container;

        // State
        this.selectedIndex = -1;        // Index in visible items
        this.selectedService = 'all';   // Current service filter
        this.focusOnServices = false;   // Is keyboard focus on services menu?
        this.selectedServiceIndex = 0;  // Index in services list

        if (!this.container) {
            console.error('ServiceFilterNav: container is required');
            return;
        }

        this.setupKeyboardNavigation();
    }

    // Get all items matching the selector
    getItems() {
        return Array.from(this.container.querySelectorAll(this.config.itemSelector));
    }

    // Get visible items based on service filter
    getVisibleItems() {
        return this.getItems().filter(item => {
            if (this.selectedService === 'all') return true;
            return item.dataset.serviceName === this.selectedService;
        });
    }

    // Get all service filter items
    getServiceItems() {
        return Array.from(this.container.querySelectorAll(this.config.serviceSelector));
    }

    // Get all marked items
    getMarkedItems() {
        return this.container.querySelectorAll(`.${this.config.markedClass}`);
    }

    // Clear selection highlighting
    clearSelection() {
        this.getItems().forEach(item => item.classList.remove(this.config.selectedClass));
        this.selectedIndex = -1;
        this.updateState();
    }

    // Select item at given index in visible list
    selectItem(index) {
        this.clearSelection();
        const items = this.getVisibleItems();
        if (index >= 0 && index < items.length) {
            this.selectedIndex = index;
            items[index].classList.add(this.config.selectedClass);
            items[index].scrollIntoView({behavior: 'smooth', block: 'nearest'});
        }
        this.updateState();
    }

    // Select service in menu at given index
    selectServiceInMenu(index) {
        const services = this.getServiceItems();
        services.forEach((s) => s.classList.remove('menu-selected'));
        if (index >= 0 && index < services.length) {
            this.selectedServiceIndex = index;
            services[index].classList.add('menu-selected');
            services[index].scrollIntoView({behavior: 'smooth', block: 'nearest'});
        }
        this.updateState();
    }

    // Apply service filter to items
    applyServiceFilter() {
        const allItems = this.getItems();
        allItems.forEach(item => {
            if (this.selectedService === 'all') {
                item.style.display = '';
            } else {
                item.style.display = item.dataset.serviceName === this.selectedService ? '' : 'none';
            }
        });
    }

    // Update debug state
    updateState() {
        if (this.config.onStateChange) {
            this.config.onStateChange({
                focusOnServices: this.focusOnServices,
                selectedIndex: this.selectedIndex,
                selectedServiceIndex: this.selectedServiceIndex,
                selectedService: this.selectedService,
            });
        }
    }

    // Focus on items list (called from external navigation like tabs)
    focusOnItems() {
        this.focusOnServices = false;
        // Don't select yet - let the keyboard handler do it on next input
    }

    // Check if we can return focus to parent (tabs)
    canReturnFocus() {
        // Can return if no item selected or at the top
        return this.selectedIndex <= 0;
    }

    setupKeyboardNavigation() {
        const handleKeyDown = (e) => {
            // Only handle if this tab is active
            const tabContent = this.container.closest('.tab-content');
            if (!tabContent || !tabContent.classList.contains('active')) return;

            const services = this.getServiceItems();

            if (this.focusOnServices) {
                this.handleServicesKeydown(e, services);
            } else {
                this.handleItemsKeydown(e);
            }
        };

        document.addEventListener('keydown', handleKeyDown);
    }

    handleServicesKeydown(e, services) {
        if (e.key === 'ArrowDown') {
            e.preventDefault();
            if (this.selectedServiceIndex < services.length - 1) {
                this.selectServiceInMenu(this.selectedServiceIndex + 1);
            }
        } else if (e.key === 'ArrowUp') {
            e.preventDefault();
            if (this.selectedServiceIndex > 0) {
                this.selectServiceInMenu(this.selectedServiceIndex - 1);
            }
        } else if (e.key === 'ArrowRight') {
            e.preventDefault();
            // Switch focus from services to items
            this.focusOnServices = false;
            services.forEach(s => s.classList.remove('menu-selected'));
            const visibleItems = this.getVisibleItems();
            if (visibleItems.length > 0) {
                this.selectItem(0);
            }
            this.updateState();
        } else if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault();
            // Select this service and stay in services menu
            const service = services[this.selectedServiceIndex];
            if (service) {
                this.selectedService = service.dataset.service;
                this.selectedIndex = -1;
                this.applyServiceFilter();

                // Update UI to show this service is active
                services.forEach(s => s.classList.remove('active'));
                service.classList.add('active');
                this.updateState();
            }
        }
    }

    handleItemsKeydown(e) {
        // Always allow Left arrow to switch to services menu
        if (e.key === 'ArrowLeft') {
            e.preventDefault();
            this.focusOnServices = true;
            this.clearSelection();
            this.selectServiceInMenu(this.selectedServiceIndex);
            return;
        }

        const items = this.getVisibleItems();
        if (items.length === 0) return;

        if (e.key === 'ArrowDown') {
            e.preventDefault();
            if (this.selectedIndex === -1) {
                this.selectItem(0);
            } else if (this.selectedIndex < items.length - 1) {
                this.selectItem(this.selectedIndex + 1);
            }
        } else if (e.key === 'ArrowUp') {
            e.preventDefault();
            if (this.selectedIndex > 0) {
                this.selectItem(this.selectedIndex - 1);
            } else if (this.selectedIndex === 0) {
                this.clearSelection();
            }
        } else if (e.key === ' ') {
            e.preventDefault();
            if (this.selectedIndex >= 0) {
                const visibleItems = this.getVisibleItems();
                if (this.selectedIndex < visibleItems.length) {
                    visibleItems[this.selectedIndex].classList.toggle(this.config.markedClass);
                }
            }
        } else if (e.key === 'Enter') {
            e.preventDefault();
            this.handleEnter();
        }
    }

    async handleEnter() {
        const markedItems = this.getMarkedItems();
        const itemsToMark = [];

        if (markedItems.length === 0) {
            // No marked items, mark the selected one
            if (this.selectedIndex >= 0) {
                const visibleItems = this.getVisibleItems();
                if (this.selectedIndex < visibleItems.length) {
                    itemsToMark.push(visibleItems[this.selectedIndex]);
                }
            }
        } else {
            // Mark all marked items
            itemsToMark.push(...Array.from(markedItems));
        }

        // Call the callback for each item to mark
        for (const item of itemsToMark) {
            if (this.config.markItemCallback) {
                await this.config.markItemCallback(item);
            }
        }
    }
}
